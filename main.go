package main

import "github.com/giantswarm/linkmeup/cmd"

func main() {
	err := cmd.Execute()
	if err != nil {
		panic(err)
	}
}
