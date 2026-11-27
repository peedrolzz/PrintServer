# PrintServer

Serviço pequeno que transforma uma Raspberry Pi 3 em uma impressora USB
compartilhada na rede local. É uma solução simples e temporária: sem nuvem,
sem aplicativo, sem portal de fornecedor e sem dependências externas.

## O que faz

- Publica as impressoras USB conectadas na Pi nas portas TCP `9100`-`9199`.
  Por padrão, qualquer máquina da mesma rede pode imprimir, como em uma
  impressora comum. O modo com token é opcional.
- Serve um painel HTTPS simples para configuração. A senha inicial é
  `printserver` e deve ser trocada no primeiro acesso.
- Sobe um hotspot WiFi chamado `PrintServer-XXXX` para configurar o
  equipamento pelo celular, sem cabo. O hotspot cai quando a Pi entra na
  rede escolhida e volta automaticamente se a conexão falhar.
- Mostra no painel o estado da rede, da USB e da impressão, além de ações
  de manutenção e teste de impressão.

## Componentes

| Binário | Função |
|---|---|
| `printserver-portal` | Painel HTTPS na porta 443, com ajudante HTTP na 80 para o portal cativo. Faz login, WiFi, teste de impressão, senhas, diagnóstico e reinício. |
| `printserver-bridge` | TCP 9100-9199. Por padrão escreve direto em `/dev/printserver/by-serial/<serial>`; se o modo com token estiver ativo, exige um handshake HMAC antes de aceitar dados. |
| `printserver-discover` | Serviço pontual acionado pela udev quando uma impressora USB é conectada. Atualiza `printers.json`. |
| `printserver-helper` | Serviço root com socket Unix e lista fixa de ações permitidas, como reiniciar serviços e trocar o modo da impressão. |
| `printserver-tool` | Pequenos comandos usados pelo primeiro boot. |

Portal e bridge rodam como o usuário sem privilégios `printserver`.
Helper, watchdog, descoberta USB e serviços de boot e rede rodam como `root`
porque precisam mexer em systemd, rede e dispositivos.

## Como usar

### 1. Primeiro boot

Grave a imagem, ligue a Pi e aguarde o boot. No primeiro boot o equipamento:

- Gera um identificador e define o nome `printserver-XXXX.local`, usando os
  últimos 4 caracteres do identificador, iguais aos do WiFi.
- Cria um certificado TLS autoassinado.
- Define a senha inicial `printserver` para o painel e para o hotspot.
- Marca o primeiro acesso, obrigando a trocar a senha do painel.
- Sobe o hotspot `PrintServer-XXXX`.

### 2. Configuração pelo celular

1. Conecte no WiFi `PrintServer-XXXX` com a senha `printserver`.
2. O portal cativo abre o painel em `https://printserver-XXXX.local`. Se não
   abrir, digite esse endereço no navegador.
3. Entre com a senha `printserver` e cadastre uma nova senha de pelo menos
   8 caracteres.
4. A senha do hotspot continua `printserver` até você trocá-la na tela de
   WiFi, se quiser.
5. Na tela de WiFi, escolha a rede desejada e informe as credenciais. Em
   redes corporativas 802.1X, o certificado é opcional.
6. Ao conectar, o hotspot cai. Na mesma rede, abra
   `https://printserver-XXXX.local` para voltar ao painel.

O endereço `printserver-XXXX.local` funciona no hotspot e na rede final
quando o aparelho resolve nomes `.local`. Se não resolver, use o IP atual da
Pi, que aparece no diagnóstico do painel. O IP do hotspot existe apenas
durante a configuração.

### 3. Impressão na rede

Configure no sistema que imprime o endereço `printserver-XXXX.local` (ou o IP
da Pi, se o aparelho não resolver `.local`) e a porta `9100`, ou a porta
mostrada no painel. No modo padrão, basta abrir a conexão e escrever os bytes
do trabalho: o bridge repassa tudo para a impressora USB. Texto e comandos
PCL funcionam na RICOH; comandos ESC/POS dependem da impressora conectada.

A impressora pode demorar alguns segundos para acordar. Trabalhos enviados
nesse intervalo saem depois, geralmente juntos.

Qualquer máquina da rede pode imprimir no modo padrão. Cada endereço pode
enviar até 60 trabalhos por minuto; para ajustar, defina
`PRINTBRIDGE_MAX_PER_MIN` no ambiente do serviço `printserver-bridge`.

Se o operador ativar o modo com token no painel, o cliente precisa enviar
antes um handshake de 60 bytes: magic `CPJ1`, nonce, timestamp e
HMAC-SHA256 com o segredo compartilhado. Só ative esse modo se o sistema que
imprime souber enviar esse handshake; se não souber, os trabalhos param.
O segredo é gerado pela própria Pi no primeiro boot, fica em
`/var/lib/printserver/credentials/bridge-token` e pode ser copiado no painel,
na tela de Impressão. Ele não é enviado automaticamente: alguém precisa
repassar esse valor a quem configura o sistema que imprime. Cada equipamento
tem o seu próprio segredo.

### 4. Recuperação

Se o acesso for perdido:

- O watchdog roda a cada 30 segundos. Se a Pi estiver como cliente do WiFi e
  não tiver IP válido por dois minutos, volta para o hotspot.
- Coloque o SD no computador, crie um arquivo vazio chamado `reset-hotspot`
  na partição de boot e devolva o cartão à Pi. No próximo boot o
  equipamento volta ao hotspot. A senha do WiFi não é alterada: se ela foi
  trocada no painel, continue usando a senha escolhida.
- Não há SSH para o operador. Recupere o acesso pelo SD no computador ou pelo
  próprio painel. O SSH na ethernet fica reservado para suporte técnico e
  exige chave pública: a imagem é selada sem login por senha e sem chaves de
  acesso. As chaves do servidor SSH são geradas de novo no primeiro boot.

## Instalação

Pacote `.deb` pronto para Raspberry Pi OS Lite 64 bits:

```
sudo apt install ./printserver_<versao>_arm64.deb
sudo reboot
```

O pacote cria o usuário do serviço, habilita os units do systemd e evita
que os serviços de distro para `hostapd` e `dnsmasq` disputem com os deste
projeto. O primeiro boot completa a configuração.

## Compilação

Requisitos: Go 1.26 ou superior e `dpkg-deb`.

```
make all        # binários arm64 em dist/bin/arm64
make deb        # pacote Debian em dist/pkg
make test       # go test ./...
make ci-scan    # auditoria simples por busca no código
```

## Segurança

Controles implementados:

- Senha por equipamento, com troca obrigatória no primeiro acesso.
- Hash Argon2id da senha administrativa.
- CSRF double-submit em todos os formulários.
- Limite de 10 senhas erradas por minuto por IP no login, limite de ações no
  painel e limite de 60 trabalhos por minuto por IP no bridge, ajustável com
  `PRINTBRIDGE_MAX_PER_MIN`.
- HSTS, CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy,
  Permissions-Policy e sem cabeçalho `Server`.
- Cookie de sessão `HttpOnly`, `Secure`, `SameSite=Strict`.
- DNS do hotspot responde apenas por `printserver.local`,
  `printserver-XXXX.local`, `printserver` e pelas sondas de portal cativo.
  Qualquer outro nome recebe REFUSED.
- SSH bloqueado na interface do hotspot e limitado à ethernet.
- Impressão aberta por padrão. O modo com token é opt-in e rejeita
  conexões sem o handshake HMAC correto.
- Portal e bridge rodando como `printserver`, com `ProtectSystem=strict`,
  `NoNewPrivileges` e capacidades mínimas. Helper, watchdog, descoberta USB e
  serviços de boot e rede rodam como `root` com escopo limitado; o primeiro
  boot não recebe esse endurecimento.
- O helper root usa lista fixa de comandos, sem shell e com socket restrito
  ao grupo `printserver`.
- No primeiro boot as chaves SSH da imagem são descartadas e geradas de novo;
  o `user-data` do cloud-init deixado pelo `rpi-imager` é apagado.
- `scripts/audit.sh` repete essas checagens no equipamento em operação.

A impressão aberta é uma escolha consciente: qualquer dispositivo que
alcance a Pi na rede pode imprimir. Se isso não for desejado, ative o modo
com token no painel. O segredo aparece na tela de Impressão somente para
quem estiver logado, então a senha do painel continua sendo o ponto de
proteção.

## Estrutura

```
cmd/              binários (portal, bridge, discover, helper, tool)
pkg/              código reutilizável
scripts/          primeiro boot, rede, WiFi, watchdog, recuperação, auditoria
systemd/          serviços e timers
config/           regras padrão de nftables
udev/             regras para impressoras USB
```

## Licença

MIT. Veja `LICENSE`.
