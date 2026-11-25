package version

var (
	Version = "0.1.0-dev"
	Commit = "unknown"
	BuildDate = "unknown"
)

func String() string {
	return "printserver " + Version + " (" + Commit + ", " + BuildDate + ")"
}
