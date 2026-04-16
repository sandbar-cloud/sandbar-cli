package cmd

import "fmt"

type VersionCmd struct{}

func (v *VersionCmd) Run(globals *Globals) error {
	fmt.Println(globals.Version)
	return nil
}
