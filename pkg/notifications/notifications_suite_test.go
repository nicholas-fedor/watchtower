package notifications_test

import (
	"testing"

	"github.com/onsi/gomega/format"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func TestNotifications(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)

	format.CharactersAroundMismatchToInclude = 20

	ginkgo.RunSpecs(t, "Notifications Suite")
}
