package mocks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Constants for magic numbers used in mock setup.
const (
	handlersPerContainer = 3 // Estimated handlers per container: base, references, image
	assertionOffset      = 2 // Call stack offset for Gomega assertions in nested calls
)

// Returns the file contents or an error if the file isn’t found.
func getMockJSONFile(relPath string) ([]byte, error) {
	absPath, _ := filepath.Abs(relPath)
	buf, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("mock JSON file %q not found: %w", absPath, err)
	}

	return buf, nil
}

// Expects the file to exist at the given relative path; fails the test otherwise.
func RespondWithJSONFile(relPath string, statusCode int, optionalHeader ...http.Header) http.HandlerFunc {
	handler, err := respondWithJSONFile(relPath, statusCode, optionalHeader...)
	gomega.ExpectWithOffset(1, err).ShouldNot(gomega.HaveOccurred())

	return handler
}

// Returns the handler and an error if the file can’t be read.
func respondWithJSONFile(relPath string, statusCode int, optionalHeader ...http.Header) (http.HandlerFunc, error) {
	buf, err := getMockJSONFile(relPath)
	if err != nil {
		return nil, err
	}

	return ghttp.RespondWith(statusCode, buf, optionalHeader...), nil
}

// Includes handlers for the given containers, their references, and associated images.
func GetContainerHandlers(containerRefs ...*ContainerRef) []http.HandlerFunc {
	handlers := make([]http.HandlerFunc, 0, len(containerRefs)*handlersPerContainer)
	for _, containerRef := range containerRefs {
		handlers = append(handlers, getContainerFileHandler(containerRef))
		// Append handlers for referenced containers
		for _, ref := range containerRef.references {
			handlers = append(handlers, getContainerFileHandler(ref))
		}
		// Append image handler for each container’s image
		handlers = append(handlers, getImageHandler(containerRef.image.id,
			RespondWithJSONFile(containerRef.image.getFileName(), http.StatusOK),
		))
	}

	return handlers
}

// Adds each status as a filter key for Docker API compatibility.
func createFilterArgs(statuses []string) filters.Args {
	args := filters.NewArgs()
	for _, status := range statuses {
		args.Add("status", status)
	}

	return args
}

// Represents a standard Watchtower image.
var defaultImage = imageRef{
	id:   types.ImageID("sha256:4dbc5f9c07028a985e14d1393e849ea07f68804c4293050d5a641b138db72daa"), // Watchtower image ID
	file: "default",
}

// Mock container fixture representing a Watchtower instance.
var Watchtower = ContainerRef{
	name:  "watchtower",
	id:    "3d88e0e3543281c747d88b27e246578b65ae8964ba86c7cd7522cf84e0978134",
	image: &defaultImage,
}

// Mock container fixture in a stopped state.
var Stopped = ContainerRef{
	name:  "stopped",
	id:    "ae8964ba86c7cd7522cf84e09781343d88e0e3543281c747d88b27e246578b65",
	image: &defaultImage,
}

// Mock container fixture in a running state with a Portainer image.
var Running = ContainerRef{
	name: "running",
	id:   "b978af0b858aa8855cce46b628817d4ed58e58f2c4f66c9b9c5449134ed4c008",
	image: &imageRef{
		id:   types.ImageID("sha256:19d07168491a3f9e2798a9bed96544e34d57ddc4757a4ac5bb199dea896c87fd"), // Portainer image ID
		file: "running",
	},
}

// Mock container fixture in a restarting state.
var Restarting = ContainerRef{
	name:  "restarting",
	id:    "ae8964ba86c7cd7522cf84e09781343d88e0e3543281c747d88b27e246578b67",
	image: &defaultImage,
}

// Mock container fixture supplying a network.
var netSupplierOK = ContainerRef{
	id:   "25e75393800b5c450a6841212a3b92ed28fa35414a586dec9f2c8a520d4910c2",
	name: "net_supplier",
	image: &imageRef{
		id:   types.ImageID("sha256:c22b543d33bfdcb9992cbef23961677133cdf09da71d782468ae2517138bad51"), // Gluetun image ID
		file: "net_producer",
	},
}

// Mock container fixture for a non-existent network supplier.
var netSupplierNotFound = ContainerRef{
	id:        NetSupplierNotFoundID,
	name:      netSupplierOK.name,
	isMissing: true,
}

// Mock container fixture consuming an existing network supplier.
var NetConsumerOK = ContainerRef{
	id:   "1f6b79d2aff23244382026c76f4995851322bed5f9c50631620162f6f9aafbd6",
	name: "net_consumer",
	image: &imageRef{
		id:   types.ImageID("sha256:904b8cb13b932e23230836850610fa45dce9eb0650d5618c2b1487c2a4f577b8"), // Nginx image ID
		file: "net_consumer",
	},
	references: []*ContainerRef{&netSupplierOK},
}

// Mock container fixture referencing a non-existent network supplier.
var NetConsumerInvalidSupplier = ContainerRef{
	id:         NetConsumerOK.id,
	name:       "net_consumer-missing_supplier",
	image:      NetConsumerOK.image,
	references: []*ContainerRef{&netSupplierNotFound},
}

const (
	NetSupplierNotFoundID    = "badc1dbadc1dbadc1dbadc1dbadc1dbadc1dbadc1dbadc1dbadc1dbadc1dbadc"
	NetSupplierContainerName = "/wt-contnet-producer-1"
)

// Fails the test if the file can’t be retrieved; returns a 404 handler if the container is missing.
func getContainerFileHandler(container *ContainerRef) http.HandlerFunc {
	if container.isMissing {
		return containerNotFoundResponse(string(container.id))
	}

	containerFile, err := container.getContainerFile()
	if err != nil {
		ginkgo.Fail(fmt.Sprintf("Failed to get container mock file: %v", err))
	}

	return getContainerHandler(
		string(container.id),
		RespondWithJSONFile(containerFile, http.StatusOK),
	)
}

// Verifies the request matches the expected container ID and applies the response handler.
func getContainerHandler(containerID string, responseHandler http.HandlerFunc) http.HandlerFunc {
	return ghttp.CombineHandlers(
		ghttp.VerifyRequest("GET", gomega.HaveSuffix("/containers/%v/json", containerID)),
		responseHandler,
	)
}

// Returns a 404 if containerInfo is nil; otherwise, serves the provided info.
func GetContainerHandler(containerID string, containerInfo *container.InspectResponse) http.HandlerFunc {
	responseHandler := containerNotFoundResponse(containerID)
	if containerInfo != nil {
		responseHandler = ghttp.RespondWithJSONEncoded(http.StatusOK, containerInfo)
	}

	return getContainerHandler(containerID, responseHandler)
}

// Serves the provided image info as a JSON response.
func GetImageHandler(imageInfo *image.InspectResponse) http.HandlerFunc {
	return getImageHandler(types.ImageID(imageInfo.ID), ghttp.RespondWithJSONEncoded(http.StatusOK, imageInfo))
}

// Filters containers by the given statuses and serves the filtered list.
func ListContainersHandler(statuses ...string) http.HandlerFunc {
	filterArgs := createFilterArgs(statuses)
	bytes, err := filterArgs.MarshalJSON()
	gomega.ExpectWithOffset(1, err).ShouldNot(gomega.HaveOccurred())

	query := url.Values{
		"filters": []string{string(bytes)},
	}

	return ghttp.CombineHandlers(
		ghttp.VerifyRequest("GET", gomega.HaveSuffix("containers/json"), query.Encode()),
		respondWithFilteredContainers(filterArgs),
	)
}

// Loads mock data from containers.json and filters it according to the provided args.
func respondWithFilteredContainers(filters filters.Args) http.HandlerFunc {
	containersJSON, err := getMockJSONFile("./mocks/data/containers.json")
	gomega.ExpectWithOffset(assertionOffset, err).ShouldNot(gomega.HaveOccurred()) // Offset for nested call depth

	var filteredContainers []container.Summary

	var containers []container.Summary

	gomega.ExpectWithOffset(assertionOffset, json.Unmarshal(containersJSON, &containers)).To(gomega.Succeed()) // Offset for nested call depth

	for _, v := range containers {
		for _, key := range filters.Get("status") {
			if v.State == key {
				filteredContainers = append(filteredContainers, v)
			}
		}
	}

	return ghttp.RespondWithJSONEncoded(http.StatusOK, filteredContainers)
}

// Verifies the request matches the expected image ID and applies the response handler.
func getImageHandler(imageID types.ImageID, responseHandler http.HandlerFunc) http.HandlerFunc {
	return ghttp.CombineHandlers(
		ghttp.VerifyRequest("GET", gomega.HaveSuffix("/images/%s/json", imageID)),
		responseHandler,
	)
}

// Returns 204 if found, 404 if not.
func KillContainerHandler(containerID string, found FoundStatus) http.HandlerFunc {
	responseHandler := noContentStatusResponse
	if !found {
		responseHandler = containerNotFoundResponse(containerID)
	}

	return ghttp.CombineHandlers(
		ghttp.VerifyRequest("POST", gomega.HaveSuffix("containers/%s/kill", containerID)),
		responseHandler,
	)
}

// Returns 204 if found, 404 if not.
func RemoveContainerHandler(containerID string, found FoundStatus) http.HandlerFunc {
	responseHandler := noContentStatusResponse
	if !found {
		responseHandler = containerNotFoundResponse(containerID)
	}

	return ghttp.CombineHandlers(
		ghttp.VerifyRequest("DELETE", gomega.HaveSuffix("containers/%s", containerID)),
		responseHandler,
	)
}

// Includes a standard "No such container" message with the ID.
func containerNotFoundResponse(containerID string) http.HandlerFunc {
	return ghttp.RespondWithJSONEncoded(http.StatusNotFound, struct{ message string }{message: "No such container: " + containerID})
}

// Mock response fixture for no-content status (204).
var noContentStatusResponse = ghttp.RespondWith(http.StatusNoContent, nil)

type FoundStatus bool

const (
	Found   FoundStatus = true
	Missing FoundStatus = false
)

// Simulates image removal with optional parent images; returns 404 if not found.
func RemoveImageHandler(imagesWithParents map[string][]string) http.HandlerFunc {
	return ghttp.CombineHandlers(
		ghttp.VerifyRequest("DELETE", gomega.MatchRegexp("/images/.*")),
		func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Split(r.URL.Path, `/`)

			targetImage := parts[len(parts)-1]
			if parents, found := imagesWithParents[targetImage]; found {
				items := []image.DeleteResponse{
					{Untagged: targetImage},
					{Deleted: targetImage},
				}
				for _, parent := range parents {
					items = append(items, image.DeleteResponse{Deleted: parent})
				}

				ghttp.RespondWithJSONEncoded(http.StatusOK, items)(w, r)
			} else {
				ghttp.RespondWithJSONEncoded(http.StatusNotFound, struct{ message string }{
					message: "Something went wrong.",
				})(w, r)
			}
		},
	)
}
