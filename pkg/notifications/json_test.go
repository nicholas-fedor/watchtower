package notifications

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nicholas-fedor/watchtower/pkg/session"
)

var _ = ginkgo.Describe("JSON template", func() {
	ginkgo.When("using report templates", func() {
		ginkgo.When("JSON template is used", func() {
			ginkgo.It("should format the messages to the expected format", func() {
				expected := `{
	"entries": [
			{
				"data": null,
				"level": "info",
				"message": "foo Bar",
				"time": "0001-01-01T00:00:00Z"
			}
		],
		"host": "Mock",
		"report": {
		"failed": [
			{
				"currentImageId": "01d210000000",
				"error": "accidentally the whole container",
				"id": "c79210000000",
				"imageName": "mock/fail1:latest",
				"latestImageId": "d0a210000000",
				"name": "fail1",
				"state": "Failed"
			}
		],
		"fresh": [
			{
				"currentImageId": "01d310000000",
				"id": "c79310000000",
				"imageName": "mock/frsh1:latest",
				"latestImageId": "01d310000000",
				"name": "frsh1",
				"state": "Fresh"
			}
		],
		"scanned": [
			{
				"currentImageId": "01d110000000",
				"id": "c79110000000",
				"imageName": "mock/updt1:latest",
				"latestImageId": "d0a110000000",
				"name": "updt1",
				"state": "Updated"
			},
			{
				"currentImageId": "01d120000000",
				"id": "c79120000000",
				"imageName": "mock/updt2:latest",
				"latestImageId": "d0a120000000",
				"name": "updt2",
				"state": "Updated"
			},
			{
				"currentImageId": "01d210000000",
				"error": "accidentally the whole container",
				"id": "c79210000000",
				"imageName": "mock/fail1:latest",
				"latestImageId": "d0a210000000",
				"name": "fail1",
				"state": "Failed"
			},
			{
				"currentImageId": "01d310000000",
				"id": "c79310000000",
				"imageName": "mock/frsh1:latest",
				"latestImageId": "01d310000000",
				"name": "frsh1",
				"state": "Fresh"
			}
		],
		"skipped": [
			{
				"currentImageId": "01d410000000",
				"error": "unpossible",
				"id": "c79410000000",
				"imageName": "mock/skip1:latest",
				"latestImageId": "01d410000000",
				"name": "skip1",
				"state": "Skipped"
			}
		],
		"stale": [],
		"updated": [
			{
				"currentImageId": "01d110000000",
				"id": "c79110000000",
				"imageName": "mock/updt1:latest",
				"latestImageId": "d0a110000000",
				"name": "updt1",
				"state": "Updated"
			},
			{
				"currentImageId": "01d120000000",
				"id": "c79120000000",
				"imageName": "mock/updt2:latest",
				"latestImageId": "d0a120000000",
				"name": "updt2",
				"state": "Updated"
			}
		]
		},
		"title": "Watchtower updates on Mock"
}`
				data := mockDataFromStates(
					session.UpdatedState,
					session.FreshState,
					session.FailedState,
					session.SkippedState,
					session.UpdatedState,
				)
				gomega.Expect(getTemplatedResult(`json.v1`, false, data)).
					To(gomega.MatchJSON(expected))
			})
			ginkgo.It("should use correct ID types in JSON output", func() {
				data := mockDataFromStates(session.UpdatedState)
				result := getTemplatedResult(`json.v1`, false, data)

				// Verify that updated containers use container IDs for the id field, and image IDs for currentImageId and latestImageId
				// Container IDs should be like "c79110000000"
				// Image IDs are like "01d110000000" and "d0a110000000"
				gomega.Expect(result).To(gomega.ContainSubstring("c79110000000"))
				gomega.Expect(result).To(gomega.ContainSubstring("01d110000000"))
				gomega.Expect(result).To(gomega.ContainSubstring("d0a110000000"))
			})
		})
	})
})
