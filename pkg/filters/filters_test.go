package filters

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/pkg/container/mocks"
)

func TestWatchtowerContainersFilter(t *testing.T) {
	t.Parallel()

	container := new(mocks.FilterableContainer)
	container.On("Name").Return("test")
	container.On("IsWatchtower").Return(true)
	assert.True(t, WatchtowerContainersFilter(container))
	container.AssertExpectations(t)
}

func TestUnscopedWatchtowerContainersFilter(t *testing.T) {
	t.Parallel()

	// Test unscoped Watchtower container (should pass)
	unscoped := new(mocks.FilterableContainer)
	unscoped.On("IsWatchtower").Return(true)
	unscoped.On("Scope").Return("", false) // No scope set
	unscoped.On("Name").Return("/unscoped-watchtower")
	assert.True(t, UnscopedWatchtowerContainersFilter(unscoped))
	unscoped.AssertExpectations(t)

	// Test explicitly scoped Watchtower container (should fail)
	scoped := new(mocks.FilterableContainer)
	scoped.On("IsWatchtower").Return(true)
	scoped.On("Scope").Return("prod", true) // Has scope
	scoped.On("Name").Return("/scoped-watchtower")
	assert.False(t, UnscopedWatchtowerContainersFilter(scoped))
	scoped.AssertExpectations(t)

	// Test none-scoped Watchtower container (should pass)
	noneScoped := new(mocks.FilterableContainer)
	noneScoped.On("IsWatchtower").Return(true)
	noneScoped.On("Scope").Return("none", true) // Explicitly none scope
	noneScoped.On("Name").Return("/none-scoped-watchtower")
	assert.True(t, UnscopedWatchtowerContainersFilter(noneScoped))
	noneScoped.AssertExpectations(t)

	// Test non-Watchtower container (should fail)
	nonWatchtower := new(mocks.FilterableContainer)
	nonWatchtower.On("IsWatchtower").Return(false)
	nonWatchtower.On("Name").Return("/regular-app")
	assert.False(t, UnscopedWatchtowerContainersFilter(nonWatchtower))
	nonWatchtower.AssertExpectations(t)
}

func TestNoFilter(t *testing.T) {
	t.Parallel()

	container := new(mocks.FilterableContainer)
	container.On("Name").Return("test")
	assert.True(t, NoFilter(container))
	container.AssertExpectations(t)
}

func TestFilterByNames(t *testing.T) {
	t.Parallel()

	var names []string

	filter := FilterByNames(names, nil)
	assert.Nil(t, filter)

	names = append(names, "test")
	filter = FilterByNames(names, NoFilter)
	assert.NotNil(t, filter)

	container := new(mocks.FilterableContainer)
	container.On("Name").Return("test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("NoTest")
	assert.False(t, filter(container))
	container.AssertExpectations(t)
}

func TestFilterByNamesLeadingSlashScenarios(t *testing.T) {
	t.Parallel()

	// Test container with normalized name matching filter without slash
	names := []string{"test"}
	filter := FilterByNames(names, NoFilter)
	assert.NotNil(t, filter)

	container := new(mocks.FilterableContainer)
	container.On("Name").Return("test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	// Test container with normalized name matching filter with slash
	names = []string{"/test"}
	filter = FilterByNames(names, NoFilter)
	assert.NotNil(t, filter)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	// Test multiple containers with normalized filter inputs
	names = []string{"container1", "container2", "container3"}
	filter = FilterByNames(names, NoFilter)
	assert.NotNil(t, filter)

	// Should match container1
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container1")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	// Should match container2
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container2")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	// Should match container3
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container3")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	// Test multiple containers with leading slash in filter inputs
	names = []string{"/container1", "/container2", "/container3"}
	filter = FilterByNames(names, NoFilter)
	assert.NotNil(t, filter)

	// Should match /container1
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container1")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	// Should match /container2
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container2")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	// Should match /container3
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container3")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	// Should not match non-matching container
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container4")
	assert.False(t, filter(container))
	container.AssertExpectations(t)
}

func TestFilterByNamesRegex(t *testing.T) {
	t.Parallel()

	names := []string{`ba(b|ll)oon`}

	filter := FilterByNames(names, NoFilter)
	assert.NotNil(t, filter)

	container := new(mocks.FilterableContainer)
	container.On("Name").Return("balloon")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("spoon")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("baboonious")
	assert.False(t, filter(container))
	container.AssertExpectations(t)
}

func TestFilterByEnableLabel(t *testing.T) {
	t.Parallel()

	filter := FilterByEnableLabel(NoFilter)
	assert.NotNil(t, filter)

	container := new(mocks.FilterableContainer)
	container.On("Enabled").Return(true, true)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Enabled").Return(false, true)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Enabled").Return(false, false)
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)
}

func TestFilterByScope(t *testing.T) {
	t.Parallel()

	scope := "testscope"

	filter := FilterByScope(scope, NoFilter)
	assert.NotNil(t, filter)

	container := new(mocks.FilterableContainer)
	container.On("Scope").Return("testscope", true)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Scope").Return("nottestscope", true)
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Scope").Return("", false)
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)
}

func TestFilterByNoneScope(t *testing.T) {
	t.Parallel()

	scope := "none"

	filter := FilterByScope(scope, NoFilter)
	assert.NotNil(t, filter)

	container := new(mocks.FilterableContainer)
	container.On("Scope").Return("anyscope", true)
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Scope").Return("", false)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Scope").Return("", true)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Scope").Return("none", true)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)
}

func TestBuildFilterNoneScope(t *testing.T) {
	t.Parallel()

	filter, desc := BuildFilter(nil, nil, false, "none")

	assert.Contains(t, desc, "without a scope")

	scoped := new(mocks.FilterableContainer)
	scoped.On("Enabled").Return(false, false)
	scoped.On("Scope").Return("anyscope", true)
	scoped.On("Name").Return("/scoped")

	unscoped := new(mocks.FilterableContainer)
	unscoped.On("Enabled").Return(false, false)
	unscoped.On("Scope").Return("", false)
	unscoped.On("Name").Return("/unscoped")

	assert.False(t, filter(scoped))
	assert.True(t, filter(unscoped))

	scoped.AssertExpectations(t)
	unscoped.AssertExpectations(t)
}

func TestFilterByDisabledLabel(t *testing.T) {
	t.Parallel()

	filter := FilterByDisabledLabel(NoFilter)
	assert.NotNil(t, filter)

	container := new(mocks.FilterableContainer)
	container.On("Enabled").Return(true, true)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Enabled").Return(false, true)
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Enabled").Return(false, false)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)
}

func TestFilterByDisableNamesLeadingSlashScenarios(t *testing.T) {
	t.Parallel()

	// Test container with normalized name excluded by filter without slash
	disableNames := []string{"excluded"}
	filter := FilterByDisableNames(disableNames, NoFilter)
	assert.NotNil(t, filter)

	container := new(mocks.FilterableContainer)
	container.On("Name").Return("excluded")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	// Test container with normalized name excluded by filter with slash
	disableNames = []string{"/excluded"}
	filter = FilterByDisableNames(disableNames, NoFilter)
	assert.NotNil(t, filter)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("excluded")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	// Test multiple containers with normalized filter inputs
	disableNames = []string{"container1", "container2", "container3"}
	filter = FilterByDisableNames(disableNames, NoFilter)
	assert.NotNil(t, filter)

	// Should exclude container1
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container1")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	// Should exclude container2
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container2")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	// Should exclude container3
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container3")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	// Test multiple containers with leading slash in filter inputs
	disableNames = []string{"/container1", "/container2", "/container3"}
	filter = FilterByDisableNames(disableNames, NoFilter)
	assert.NotNil(t, filter)

	// Should exclude /container1
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container1")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	// Should exclude /container2
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container2")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	// Should exclude /container3
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container3")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	// Should allow non-excluded container
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("container4")
	assert.True(t, filter(container))
	container.AssertExpectations(t)
}

func TestFilterByImage(t *testing.T) {
	t.Parallel()

	filterEmpty := FilterByImage(nil, NoFilter)
	filterSingle := FilterByImage([]string{"registry"}, NoFilter)
	filterMultiple := FilterByImage([]string{"registry", "bla"}, NoFilter)

	assert.NotNil(t, filterSingle)
	assert.NotNil(t, filterMultiple)

	container := new(mocks.FilterableContainer)
	container.On("ImageName").Return("registry:2")
	container.On("Name").Return("/test")
	assert.True(t, filterEmpty(container))
	assert.True(t, filterSingle(container))
	assert.True(t, filterMultiple(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("ImageName").Return("registry:latest")
	container.On("Name").Return("/test")
	assert.True(t, filterEmpty(container))
	assert.True(t, filterSingle(container))
	assert.True(t, filterMultiple(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("ImageName").Return("abcdef1234")
	container.On("Name").Return("/test")
	assert.True(t, filterEmpty(container))
	assert.False(t, filterSingle(container))
	assert.False(t, filterMultiple(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("ImageName").Return("bla:latest")
	container.On("Name").Return("/test")
	assert.True(t, filterEmpty(container))
	assert.False(t, filterSingle(container))
	assert.True(t, filterMultiple(container))
	container.AssertExpectations(t)

	filterEmptyTagged := FilterByImage(nil, NoFilter)
	filterSingleTagged := FilterByImage([]string{"registry:develop"}, NoFilter)
	filterMultipleTagged := FilterByImage([]string{"registry:develop", "registry:latest"}, NoFilter)

	assert.NotNil(t, filterSingleTagged)
	assert.NotNil(t, filterMultipleTagged)

	container = new(mocks.FilterableContainer)
	container.On("ImageName").Return("bla:latest")
	container.On("Name").Return("/test")
	assert.True(t, filterEmptyTagged(container))
	assert.False(t, filterSingleTagged(container))
	assert.False(t, filterMultipleTagged(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("ImageName").Return("registry:latest")
	container.On("Name").Return("/test")
	assert.True(t, filterEmptyTagged(container))
	assert.False(t, filterSingleTagged(container))
	assert.True(t, filterMultipleTagged(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("ImageName").Return("registry:develop")
	container.On("Name").Return("/test")
	assert.True(t, filterEmptyTagged(container))
	assert.True(t, filterSingleTagged(container))
	assert.True(t, filterMultipleTagged(container))
	container.AssertExpectations(t)
}

func TestFilterByImageMalformed(t *testing.T) {
	t.Parallel()

	filter := FilterByImage([]string{"valid:image", "invalid::tag", "image:", ":tag", ""}, NoFilter)
	assert.NotNil(t, filter)

	container := new(mocks.FilterableContainer)
	container.On("ImageName").Return("valid:image")
	container.On("Name").Return("/test")
	assert.True(t, filter(container)) // Valid match
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("ImageName").Return("valid:other")
	container.On("Name").Return("/test")
	assert.False(t, filter(container)) // Tag mismatch
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("ImageName").Return("invalid:tag")
	container.On("Name").Return("/test")
	assert.False(t, filter(container)) // Malformed input ignored
	container.AssertExpectations(t)
}

func TestBuildFilter(t *testing.T) {
	t.Parallel()

	names := []string{"test", "valid"}

	filter, desc := BuildFilter(names, []string{}, false, "")
	assert.Contains(t, desc, "test")
	assert.Contains(t, desc, "or")
	assert.Contains(t, desc, "valid")

	container := new(mocks.FilterableContainer)
	container.On("Name").Return("Invalid").Maybe()
	container.On("Enabled").Return(false, false).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("test").Maybe()
	container.On("Enabled").Return(false, false).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("Invalid").Maybe()
	container.On("Enabled").Return(true, true).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("test").Maybe()
	container.On("Enabled").Return(true, true).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Enabled").Return(false, true).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.False(t, filter(container))
	container.AssertExpectations(t)
}

func TestBuildFilterEnableLabel(t *testing.T) {
	t.Parallel()

	var names []string

	names = append(names, "test")

	filter, desc := BuildFilter(names, []string{}, true, "")
	assert.Contains(t, desc, "using enable label")

	container := new(mocks.FilterableContainer)
	container.On("Enabled").Return(false, false)
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("Invalid").Maybe()
	container.On("Enabled").Return(true, true).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("test").Maybe()
	container.On("Enabled").Return(true, true).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Enabled").Return(false, true).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.False(t, filter(container))
	container.AssertExpectations(t)
}

func TestBuildFilterDisableContainer(t *testing.T) {
	t.Parallel()

	filter, desc := BuildFilter([]string{}, []string{"excluded", "notfound"}, false, "")
	assert.Contains(t, desc, "not named")
	assert.Contains(t, desc, "excluded")
	assert.Contains(t, desc, "or")
	assert.Contains(t, desc, "notfound")

	container := new(mocks.FilterableContainer)
	container.On("Name").Return("Another").Maybe()
	container.On("Enabled").Return(false, false).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("AnotherOne").Maybe()
	container.On("Enabled").Return(true, true).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("test").Maybe()
	container.On("Enabled").Return(false, false).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("excluded").Maybe()
	container.On("Enabled").Return(true, true).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("excludedAsSubstring").Maybe()
	container.On("Enabled").Return(true, true).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("notfound").Maybe()
	container.On("Enabled").Return(true, true).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Enabled").Return(false, true).Maybe()
	container.On("Scope").Return("", false).Maybe() // No scope set, defaults to "none"
	container.On("Name").Return("/test").Maybe()
	assert.False(t, filter(container))
	container.AssertExpectations(t)
}
