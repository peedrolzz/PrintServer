// Package printprofile escolhe o filtro usado pelo CUPS para cada impressora.
//
// A meta é atender a maioria das impressoras sem instalar nada na máquina de
// quem imprime. O PDF é convertido na própria Pi; o perfil só diz qual
// linguagem a impressora entende. Se a escolha automática errar, o operador
// troca o perfil no painel, sem mexer no computador que envia o trabalho.
package printprofile

import (
	"regexp"
	"strings"
)

const (
	PCL        = "pcl"
	PCLXL      = "pclxl"
	PostScript = "postscript"
	Epson      = "epson"
	GDI        = "gdi"
)

type Option struct {
	Name  string
	Label string
}

var options = []Option{
	{PCL, "PCL (padrão)"},
	{PCLXL, "PCL XL"},
	{PostScript, "PostScript"},
	{Epson, "ESC/P (Epson)"},
	{GDI, "GDI (Samsung)"},
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Options devolve a lista ordenada para o seletor do painel.
func Options() []Option {
	out := make([]Option, len(options))
	copy(out, options)
	return out
}

// Valid diz se o perfil é um dos que o pacote sabe instalar.
func Valid(name string) bool {
	for _, o := range options {
		if o.Name == name {
			return true
		}
	}
	return false
}

// Choose faz a escolha inicial a partir dos dados que o udev consegue ler da
// impressora. Na dúvida, PCL atende a maioria dos laserjets, incluindo Ricoh,
// HP, Kyocera, Lexmark, Brother e Canon.
func Choose(manufacturer, model, usbID string) string {
	id := strings.ToUpper(manufacturer + " " + model + " " + usbID)

	switch {
	case strings.Contains(id, "EPSON") || strings.HasPrefix(id, "04B8:"):
		return Epson
	case strings.Contains(id, "SAMSUNG") || strings.HasPrefix(id, "04E8:"):
		return GDI
	case strings.Contains(id, "POSTSCRIPT") || strings.Contains(id, "PSPRINTER"):
		return PostScript
	case strings.Contains(id, "PCLXL") || strings.Contains(id, "PCL 6"):
		return PCLXL
	default:
		return PCL
	}
}

// Effective devolve o perfil salvo quando ele é válido. Config antigas sem
// perfil caem na escolha automática.
func Effective(saved, manufacturer, model, usbID string) string {
	if Valid(saved) {
		return saved
	}
	return Choose(manufacturer, model, usbID)
}

// QueueName gera o nome da fila CUPS a partir do serial. O nome é estável e
// seguro para lpadmin, avahi e URL IPP.
func QueueName(serial string) string {
	s := strings.ToLower(serial)
	s = strings.Map(func(r rune) rune {
		switch r {
		case 'à', 'á', 'â', 'ã', 'ä', 'å':
			return 'a'
		case 'è', 'é', 'ê', 'ë':
			return 'e'
		case 'ì', 'í', 'î', 'ï':
			return 'i'
		case 'ò', 'ó', 'ô', 'õ', 'ö':
			return 'o'
		case 'ù', 'ú', 'û', 'ü':
			return 'u'
		case 'ç':
			return 'c'
		case 'ñ':
			return 'n'
		}
		return r
	}, s)
	s = nonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "printserver"
	}
	if len(s) > 48 {
		s = s[:48]
	}
	return "ps-" + s
}

// PPDPath aponta para o arquivo instalado junto com o pacote.
func PPDPath(profile string) string {
	return "/usr/share/ppd/printserver/PrintServer-" + strings.ToUpper(profile) + ".ppd"
}
