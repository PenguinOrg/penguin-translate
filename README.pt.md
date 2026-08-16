# Penguin Translate (Translation Overlay)

[简体中文](README.md) · [繁體中文](README.zh-TW.md) · [粵語](README.yue.md) · [日本語](README.ja.md) · [Français](README.fr.md) · [Português](README.pt.md)

Penguin Translate é um aplicativo de tradução em tempo real para Windows. Ele traduz o texto exibido em outra janela, transcreve e traduz o áudio do sistema, processa a voz do microfone e permite praticar a pronúncia de uma tradução.

![Exemplo de tradução OCR de uma janela: interface original à esquerda e tradução sobreposta à direita](docs/media/window-ocr-demo.svg)

## Recursos

- **Tradução de janela**: o aplicativo reconhece e traduz o texto da janela selecionada e sobrepõe a tradução à tela. Os cliques continuam chegando à janela original. Esse modo funciona na área de trabalho e no SteamVR e pode ser ativado ou desativado por um atalho de teclado.
- **Legendas do áudio do sistema**: o aplicativo transcreve e traduz o som reproduzido pelo computador e exibe o texto original e a tradução em uma barra de legendas separada.
- **Tradução pelo microfone**: a voz captada pelo microfone é transcrita e traduzida em tempo real.
- **Prática de pronúncia**: o aplicativo reproduz uma tradução, grava sua leitura e avalia a tentativa. Há recursos de leitura para japonês, chinês e coreano.

![Janela principal do Penguin Translate: tradução de uma voz em chinês para japonês e coreano, seguida da transcrição e retradução das respostas para chinês](docs/media/app-window-demo.svg)

## Prática de pronúncia

Em uma conversa, selecione “Praticar esta frase” para abrir a tradução correspondente na tela de prática. Ouça o exemplo, mantenha o botão de gravação pressionado enquanto lê e solte-o para ver a pontuação geral e o texto reconhecido. Com a avaliação do Penguin Cloud, a tela também mostra o feedback de cada palavra, a precisão, a fluência e a completude.

![Tela de prática: seleção de uma tradução em japonês, reprodução do exemplo, gravação e resultado](docs/media/practice-mode-demo.svg)

## Idiomas disponíveis

O seletor de idiomas inclui inglês, japonês, chinês simplificado, chinês tradicional, cantonês, wu, coreano, espanhol, francês, alemão, italiano, português e russo.
