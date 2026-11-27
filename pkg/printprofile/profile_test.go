package printprofile

import "testing"

func TestChoose(t *testing.T) {
	cases := []struct {
		manufacturer, model, usbID, want string
	}{
		{"RICOH", "SP 310", "05ca:0442", PCL},
		{"HP", "LaserJet Pro", "03f0:1234", PCL},
		{"EPSON", "LX-350", "04b8:0001", Epson},
		{"Samsung", "ML-2160", "04e8:1234", GDI},
		{"Generic", "PostScript Printer", "", PostScript},
		{"Kyocera", "FS-1040", "0482:1234", PCL},
	}
	for _, c := range cases {
		if got := Choose(c.manufacturer, c.model, c.usbID); got != c.want {
			t.Errorf("Choose(%q,%q,%q) = %q, want %q", c.manufacturer, c.model, c.usbID, got, c.want)
		}
	}
}

func TestEffective(t *testing.T) {
	if got := Effective("", "EPSON", "LX", "04b8:1"); got != Epson {
		t.Fatalf("perfil vazio deve usar detecção: %q", got)
	}
	if got := Effective(PostScript, "EPSON", "LX", "04b8:1"); got != PostScript {
		t.Fatalf("perfil salvo deve ser respeitado: %q", got)
	}
	if got := Effective("inventado", "RICOH", "SP", ""); got != PCL {
		t.Fatalf("perfil inválido deve cair no padrão: %q", got)
	}
}

func TestValid(t *testing.T) {
	for _, o := range Options() {
		if !Valid(o.Name) {
			t.Errorf("opção %q não é válida", o.Name)
		}
	}
	if Valid("") || Valid("qualquer") {
		t.Fatal("perfil desconhecido não pode ser aceito")
	}
}

func TestQueueName(t *testing.T) {
	cases := map[string]string{
		"RHAE7BAA":  "ps-rhae7baa",
		"Série/001": "ps-serie-001",
		"!!!":       "printserver",
		"ABCDEFGHIJABCDEFGHIJABCDEFGHIJABCDEFGHIJABCDEFGHIJABCDEFGHIJ": "ps-" + "abcdefghijabcdefghijabcdefghijabcdefghijabcdefgh",
	}
	for in, want := range cases {
		if got := QueueName(in); got != want {
			t.Errorf("QueueName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPPDPath(t *testing.T) {
	if got := PPDPath(PCL); got != "/usr/share/ppd/printserver/PrintServer-PCL.ppd" {
		t.Fatalf("caminho inesperado: %q", got)
	}
}
