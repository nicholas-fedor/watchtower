package main

import (
	"github.com/nicholas-fedor/watchtower/cmd"
	"github.com/sirupsen/logrus"
)

func init() {
	logrus.SetLevel(logrus.InfoLevel)
}

func main() {
	cmd.Execute()
}
