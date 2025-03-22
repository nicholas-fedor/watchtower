package main

import (
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/cmd"
)

func init() {
	logrus.SetLevel(logrus.InfoLevel)
}

func main() {
	cmd.Execute()
}
