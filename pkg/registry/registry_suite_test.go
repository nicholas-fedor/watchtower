package registry_test

import (
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func TestRegistry(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	logrus.SetOutput(ginkgo.GinkgoWriter)
	ginkgo.RunSpecs(t, "Registry Suite")
}
