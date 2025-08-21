package version

import "fmt"

var (
	Name    = "mycoder"
	Version = "0.1.0"
	Commit  = "dev"
	Date    = ""
)

func String() string {
	return fmt.Sprintf("%s %s (%s)", Name, Version, Commit)
}
