package notifications_test

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

func TestNotifications(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)

	format.CharactersAroundMismatchToInclude = 20

	ginkgo.RunSpecs(t, "Notifications Suite")
}
