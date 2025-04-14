package filters

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/pkg/container/mocks"
)

func TestWatchtowerContainersFilter(t *testing.T) {
	t.Parallel()

	container := new(mocks.FilterableContainer)
	container.On("Name").Return("/test")
	container.On("IsWatchtower").Return(true)
	assert.True(t, WatchtowerContainersFilter(container))
	container.AssertExpectations(t)
}

func TestNoFilter(t *testing.T) {
	t.Parallel()

	container := new(mocks.FilterableContainer)
	container.On("Name").Return("/test")
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
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)
	container = new(mocks.FilterableContainer)
	container.On("Name").Return("/NoTest")
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
	container.On("Name").Return("Invalid")
	container.On("Enabled").Return(false, false)
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("test")
	container.On("Enabled").Return(false, false)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("Invalid")
	container.On("Enabled").Return(true, true)
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("test")
	container.On("Enabled").Return(true, true)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Enabled").Return(false, true)
	container.On("Name").Return("/test")
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
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("Invalid")
	container.On("Enabled").Twice().Return(true, true)
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("test")
	container.On("Enabled").Twice().Return(true, true)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Enabled").Return(false, true)
	container.On("Name").Return("/test")
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
	container.On("Name").Return("Another")
	container.On("Enabled").Return(false, false)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("AnotherOne")
	container.On("Enabled").Return(true, true)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("test")
	container.On("Enabled").Return(false, false)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("excluded")
	container.On("Enabled").Return(true, true)
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("excludedAsSubstring")
	container.On("Enabled").Return(true, true)
	container.On("Name").Return("/test")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Name").Return("notfound")
	container.On("Enabled").Return(true, true)
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	container = new(mocks.FilterableContainer)
	container.On("Enabled").Return(false, true)
	container.On("Name").Return("/test")
	assert.False(t, filter(container))
	container.AssertExpectations(t)
}
