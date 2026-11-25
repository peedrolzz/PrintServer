# PrintServer · sistema visual do painel

Decisões fixadas em 25/11/2026. Use como fonte única ao mexer nos templates.
Implementação de referência: `cmd/portal/templates/base.html`.

## Direção
Papel térmico + LED. O painel é um aparelho de balcão, não um dashboard.
O operador está no celular, em pé, e quer trocar a rede ou imprimir um teste
em poucos toques. Nada de fontes externas: a Pi pode estar sem internet.

## Assinatura
Cabeçalho em monoespaçado com traço tracejado e o sufixo de 4 caracteres do
SSID (ex.: `F39F`). Dados em linhas de cupom: rótulo à esquerda, valor à
direita, traço pontilhado. Botões são teclas grandes e quadradas.
`PrintServer` fica sem sublinhado, inclusive no hover.

## Paleta
Tema claro forçado (`color-scheme: light`). O painel não segue o modo escuro
do celular, para não aparecer verde no login e amarelo no painel.

- Papel `#FFFFFF`, mesa `#F1F1F1`, tinta `#141414`, secundária `#4A4A4A`,
  terciária `#757575`, régua `#DCDCDD`.
- Verde `#0F8A51` (texto `#0A6E40`, fundo `#EAF6EF`) só para pronto/OK.
- Âmbar `#8A5A00` (fundo `#FBF2DD`) para atenção.
- Vermelho `#B3261E` (fundo `#FCECEA`) só para falha e ação destrutiva.
- Sem azul, sem gradiente, sem múltiplos acentos.

## Profundidade
Somente bordas de 1px e traços tracejados/pontilhados. Sem sombras, sem
glassmorphism, sem cards empilhados. Uma única superfície de papel.

## Tipografia
System UI para texto e `ui-monospace` para IDs, IPs, portas e valores.
Nenhuma fonte baixada da rede. Sem footer com número de versão.

## Espaçamento
Base 4px. Escala 4/8/12/16/20/24. Alvos de toque com no mínimo 44px.

## Componentes
- `.btn`: largura total, 46px de altura, raio 6px. `.primary` é a única ação
  principal da tela. `.danger` só em reinício/liberação de impressão.
- `.segment`: seletor de três opções, 44px+ por opção.
- `.rows`: lista de dados em estilo cupom.
- `.msg`: feedback curto, nunca um parágrafo de instruções.
- `.led`: ponto de estado antes do título. `.ok`, `.warn` e `.err` coloram
  apenas esse ponto.
- Ações de Manutenção aparecem direto na tela, sob um título `h2`. Nunca
  usar `details`/`summary` para esconder botões principais.

## Navegação
No cabeçalho, `Voltar` ocupa o lugar de `Início` nas páginas internas e leva
ao painel principal. Na tela inicial e no primeiro acesso não há `Voltar`.

## Endereço e primeiro acesso
Antes da queda do hotspot, mostrar sempre `printserver-<id>.local`, nunca
`10.42.0.1`. O IP do hotspot deixa de existir quando o equipamento troca de
rede. Vale para o portal cativo e para as telas `wifi_connecting`,
`wifi_scanning` e `password_wifi_saved`.

Texto da troca: `Após a conexão WiFi, o hotspot cai do ar. Conecte o celular
na mesma WiFi e acesse https://printserver-<id>.local.` Na tela de WiFi, a dica
antes do botão Conectar usa a mesma orientação, sem explicação longa.

A dica `Senha inicial: printserver` aparece no login somente enquanto o
primeiro acesso não foi concluído. Depois da troca de senha, some.

## Estado da tela inicial
O título diz o que falta: `Sem impressora`, `Sem rede` ou
`Sem rede nem impressora`. Nunca usar `Verificando`. A orientação fica logo
abaixo do título, com 8px de afastamento (classe `has-hint` no `h1.led`).
`Pronto` com verde exige rede e impressora; sem internet é aviso âmbar,
porque a impressão local continua funcionando.

## Regras de texto
Uma frase por tela no máximo fora dos dados. Sem "O que fazer agora",
sem listas de 3 passos, sem explicar o óbvio. O texto que sobrou deve ser
instrução de recuperação (ex.: o que fazer quando o hotspot cai).
Sem travessões nos textos da interface.
