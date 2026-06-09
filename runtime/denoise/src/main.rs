use std::error::Error;
use std::io::{self, Cursor, Read, Write};

use nnnoiseless::DenoiseState;

const MAX_WAV_BYTES: usize = 256 * 1024 * 1024;

fn main() {
    let mut stdin = io::stdin().lock();
    let mut stdout = io::stdout().lock();

    let kernel = make_lowpass(64, 7800.0 / 48000.0);

    let mut len_buf = [0u8; 4];
    loop {
        if stdin.read_exact(&mut len_buf).is_err() {
            return;
        }
        let n = u32::from_le_bytes(len_buf) as usize;
        if n == 0 || n > MAX_WAV_BYTES {
            eprintln!("denoise: bad frame length {n}");
            return;
        }
        let mut wav = vec![0u8; n];
        if stdin.read_exact(&mut wav).is_err() {
            return;
        }

        // Never drop a caption: any failure falls back to the original audio.
        let out = match denoise_wav(&wav, &kernel) {
            Ok(o) => o,
            Err(e) => {
                eprintln!("denoise: passthrough ({e})");
                wav
            }
        };

        if stdout.write_all(&(out.len() as u32).to_le_bytes()).is_err()
            || stdout.write_all(&out).is_err()
            || stdout.flush().is_err()
        {
            return;
        }
    }
}

// RNNoise is fixed at 48 kHz / 480-sample frames, so 16 kHz input is resampled
// 16k -> 48k -> 16k (integer factor 3, so the length round-trips exactly); any
// other rate is passed through untouched.
fn denoise_wav(wav: &[u8], kernel: &[f32]) -> Result<Vec<u8>, Box<dyn Error>> {
    let mut reader = hound::WavReader::new(Cursor::new(wav))?;
    let spec = reader.spec();
    if spec.sample_format != hound::SampleFormat::Int || spec.bits_per_sample != 16 {
        return Err("expected 16-bit integer PCM".into());
    }
    let channels = spec.channels.max(1) as usize;
    let in_rate = spec.sample_rate;

    let samples: Vec<i16> = reader.samples::<i16>().collect::<Result<_, _>>()?;
    let mono: Vec<f32> = if channels <= 1 {
        samples.iter().map(|&s| s as f32).collect()
    } else {
        samples
            .chunks(channels)
            .map(|c| c.iter().map(|&s| s as f32).sum::<f32>() / c.len() as f32)
            .collect()
    };
    if mono.is_empty() {
        return Ok(wav.to_vec());
    }

    let final_sig: Vec<f32> = match in_rate {
        48000 => denoise_48k(&mono),
        16000 => {
            let up = upsample3(&mono, kernel);
            let den = denoise_48k(&up);
            downsample3(&den, kernel)
        }
        _ => return Ok(wav.to_vec()),
    };

    let spec_out = hound::WavSpec {
        channels: 1,
        sample_rate: in_rate,
        bits_per_sample: 16,
        sample_format: hound::SampleFormat::Int,
    };
    let mut cursor = Cursor::new(Vec::<u8>::new());
    {
        let mut writer = hound::WavWriter::new(&mut cursor, spec_out)?;
        for &s in &final_sig {
            writer.write_sample(s.round().clamp(-32768.0, 32767.0) as i16)?;
        }
        writer.finalize()?;
    }
    Ok(cursor.into_inner())
}

fn denoise_48k(sig: &[f32]) -> Vec<f32> {
    let frame = DenoiseState::FRAME_SIZE;
    let mut state = DenoiseState::new();
    let mut out = vec![0f32; sig.len()];
    let mut inbuf = vec![0f32; frame];
    let mut outbuf = vec![0f32; frame];

    let mut i = 0;
    while i < sig.len() {
        let n = (sig.len() - i).min(frame);
        inbuf[..n].copy_from_slice(&sig[i..i + n]);
        for v in inbuf[n..].iter_mut() {
            *v = 0.0;
        }
        state.process_frame(&mut outbuf, &inbuf);
        out[i..i + n].copy_from_slice(&outbuf[..n]);
        i += frame;
    }
    out
}

fn make_lowpass(num_taps: usize, fc: f32) -> Vec<f32> {
    let m = (num_taps - 1) as f32;
    let pi = std::f32::consts::PI;
    let mut h = vec![0f32; num_taps];
    let mut sum = 0f32;
    for (n, tap) in h.iter_mut().enumerate() {
        let k = n as f32 - m / 2.0;
        let sinc = if k.abs() < 1e-6 {
            2.0 * fc
        } else {
            (2.0 * pi * fc * k).sin() / (pi * k)
        };
        let win = 0.54 - 0.46 * (2.0 * pi * n as f32 / m).cos();
        *tap = sinc * win;
        sum += *tap;
    }
    for tap in h.iter_mut() {
        *tap /= sum;
    }
    h
}

fn fir_same(x: &[f32], h: &[f32], gain: f32) -> Vec<f32> {
    let taps = h.len();
    let half = (taps - 1) / 2;
    let n = x.len();
    let mut y = vec![0f32; n];
    for (i, yi) in y.iter_mut().enumerate() {
        let mut acc = 0f32;
        for (j, &hj) in h.iter().enumerate() {
            let idx = i as isize + half as isize - j as isize;
            if idx >= 0 && (idx as usize) < n {
                acc += hj * x[idx as usize];
            }
        }
        *yi = acc * gain;
    }
    y
}

fn upsample3(x: &[f32], kernel: &[f32]) -> Vec<f32> {
    let mut up = vec![0f32; x.len() * 3];
    for (i, &s) in x.iter().enumerate() {
        up[i * 3] = s;
    }
    fir_same(&up, kernel, 3.0)
}

fn downsample3(x: &[f32], kernel: &[f32]) -> Vec<f32> {
    fir_same(x, kernel, 1.0)
        .into_iter()
        .step_by(3)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_wav_16k(samples: &[i16]) -> Vec<u8> {
        let spec = hound::WavSpec {
            channels: 1,
            sample_rate: 16000,
            bits_per_sample: 16,
            sample_format: hound::SampleFormat::Int,
        };
        let mut cur = Cursor::new(Vec::<u8>::new());
        {
            let mut w = hound::WavWriter::new(&mut cur, spec).unwrap();
            for &s in samples {
                w.write_sample(s).unwrap();
            }
            w.finalize().unwrap();
        }
        cur.into_inner()
    }

    fn read_mono(wav: &[u8]) -> (u32, Vec<f32>) {
        let mut r = hound::WavReader::new(Cursor::new(wav)).unwrap();
        let rate = r.spec().sample_rate;
        let s: Vec<f32> = r.samples::<i16>().map(|x| x.unwrap() as f32).collect();
        (rate, s)
    }

    fn rms(s: &[f32]) -> f32 {
        if s.is_empty() {
            return 0.0;
        }
        (s.iter().map(|&x| x * x).sum::<f32>() / s.len() as f32).sqrt()
    }

    fn noise(n: usize) -> Vec<i16> {
        let mut state: u32 = 0x1234_5678;
        (0..n)
            .map(|_| {
                state = state.wrapping_mul(1_664_525).wrapping_add(1_013_904_223);
                (state >> 16) as i16 / 8
            })
            .collect()
    }

    #[test]
    fn preserves_format_and_length() {
        let kernel = make_lowpass(64, 7800.0 / 48000.0);
        let input = make_wav_16k(&noise(16000));
        let out = denoise_wav(&input, &kernel).unwrap();
        let (rate, samples) = read_mono(&out);
        assert_eq!(rate, 16000, "output must stay 16 kHz");
        assert_eq!(samples.len(), 16000, "sample count must be preserved");
    }

    #[test]
    fn reduces_broadband_noise() {
        let kernel = make_lowpass(64, 7800.0 / 48000.0);
        let raw = noise(16000);
        let in_rms = rms(&raw.iter().map(|&s| s as f32).collect::<Vec<_>>());
        let out = denoise_wav(&make_wav_16k(&raw), &kernel).unwrap();
        let (_, samples) = read_mono(&out);
        let out_rms = rms(&samples);
        assert!(
            out_rms > 0.0 && out_rms < in_rms * 0.95,
            "expected attenuation: in_rms={in_rms} out_rms={out_rms}"
        );
    }

    #[test]
    fn silence_stays_silent() {
        let kernel = make_lowpass(64, 7800.0 / 48000.0);
        let out = denoise_wav(&make_wav_16k(&vec![0i16; 8000]), &kernel).unwrap();
        let (_, samples) = read_mono(&out);
        assert!(rms(&samples) < 1.0, "silence must remain silent");
    }
}
