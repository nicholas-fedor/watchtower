package notifications

import (
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/sirupsen/logrus"
)

// StaticData is the part of the notification template data model set upon initialization
type StaticData struct {
	Title string
	Host  string
}

// Data is the notification template data model
type Data struct {
	StaticData
	Entries []*logrus.Entry
	Report  types.Report
}
