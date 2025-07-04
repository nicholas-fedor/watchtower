// Code generated by mockery; DO NOT EDIT.
// github.com/vektra/mockery
// template: testify

package mocks

import (
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mock "github.com/stretchr/testify/mock"
)

// NewMockContainerReport creates a new instance of MockContainerReport. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockContainerReport(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockContainerReport {
	mock := &MockContainerReport{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}

// MockContainerReport is an autogenerated mock type for the ContainerReport type
type MockContainerReport struct {
	mock.Mock
}

type MockContainerReport_Expecter struct {
	mock *mock.Mock
}

func (_m *MockContainerReport) EXPECT() *MockContainerReport_Expecter {
	return &MockContainerReport_Expecter{mock: &_m.Mock}
}

// CurrentImageID provides a mock function for the type MockContainerReport
func (_mock *MockContainerReport) CurrentImageID() types.ImageID {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for CurrentImageID")
	}

	var r0 types.ImageID
	if returnFunc, ok := ret.Get(0).(func() types.ImageID); ok {
		r0 = returnFunc()
	} else {
		r0 = ret.Get(0).(types.ImageID)
	}
	return r0
}

// MockContainerReport_CurrentImageID_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'CurrentImageID'
type MockContainerReport_CurrentImageID_Call struct {
	*mock.Call
}

// CurrentImageID is a helper method to define mock.On call
func (_e *MockContainerReport_Expecter) CurrentImageID() *MockContainerReport_CurrentImageID_Call {
	return &MockContainerReport_CurrentImageID_Call{Call: _e.mock.On("CurrentImageID")}
}

func (_c *MockContainerReport_CurrentImageID_Call) Run(run func()) *MockContainerReport_CurrentImageID_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockContainerReport_CurrentImageID_Call) Return(imageID types.ImageID) *MockContainerReport_CurrentImageID_Call {
	_c.Call.Return(imageID)
	return _c
}

func (_c *MockContainerReport_CurrentImageID_Call) RunAndReturn(run func() types.ImageID) *MockContainerReport_CurrentImageID_Call {
	_c.Call.Return(run)
	return _c
}

// Error provides a mock function for the type MockContainerReport
func (_mock *MockContainerReport) Error() string {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for Error")
	}

	var r0 string
	if returnFunc, ok := ret.Get(0).(func() string); ok {
		r0 = returnFunc()
	} else {
		r0 = ret.Get(0).(string)
	}
	return r0
}

// MockContainerReport_Error_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Error'
type MockContainerReport_Error_Call struct {
	*mock.Call
}

// Error is a helper method to define mock.On call
func (_e *MockContainerReport_Expecter) Error() *MockContainerReport_Error_Call {
	return &MockContainerReport_Error_Call{Call: _e.mock.On("Error")}
}

func (_c *MockContainerReport_Error_Call) Run(run func()) *MockContainerReport_Error_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockContainerReport_Error_Call) Return(s string) *MockContainerReport_Error_Call {
	_c.Call.Return(s)
	return _c
}

func (_c *MockContainerReport_Error_Call) RunAndReturn(run func() string) *MockContainerReport_Error_Call {
	_c.Call.Return(run)
	return _c
}

// ID provides a mock function for the type MockContainerReport
func (_mock *MockContainerReport) ID() types.ContainerID {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for ID")
	}

	var r0 types.ContainerID
	if returnFunc, ok := ret.Get(0).(func() types.ContainerID); ok {
		r0 = returnFunc()
	} else {
		r0 = ret.Get(0).(types.ContainerID)
	}
	return r0
}

// MockContainerReport_ID_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'ID'
type MockContainerReport_ID_Call struct {
	*mock.Call
}

// ID is a helper method to define mock.On call
func (_e *MockContainerReport_Expecter) ID() *MockContainerReport_ID_Call {
	return &MockContainerReport_ID_Call{Call: _e.mock.On("ID")}
}

func (_c *MockContainerReport_ID_Call) Run(run func()) *MockContainerReport_ID_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockContainerReport_ID_Call) Return(containerID types.ContainerID) *MockContainerReport_ID_Call {
	_c.Call.Return(containerID)
	return _c
}

func (_c *MockContainerReport_ID_Call) RunAndReturn(run func() types.ContainerID) *MockContainerReport_ID_Call {
	_c.Call.Return(run)
	return _c
}

// ImageName provides a mock function for the type MockContainerReport
func (_mock *MockContainerReport) ImageName() string {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for ImageName")
	}

	var r0 string
	if returnFunc, ok := ret.Get(0).(func() string); ok {
		r0 = returnFunc()
	} else {
		r0 = ret.Get(0).(string)
	}
	return r0
}

// MockContainerReport_ImageName_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'ImageName'
type MockContainerReport_ImageName_Call struct {
	*mock.Call
}

// ImageName is a helper method to define mock.On call
func (_e *MockContainerReport_Expecter) ImageName() *MockContainerReport_ImageName_Call {
	return &MockContainerReport_ImageName_Call{Call: _e.mock.On("ImageName")}
}

func (_c *MockContainerReport_ImageName_Call) Run(run func()) *MockContainerReport_ImageName_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockContainerReport_ImageName_Call) Return(s string) *MockContainerReport_ImageName_Call {
	_c.Call.Return(s)
	return _c
}

func (_c *MockContainerReport_ImageName_Call) RunAndReturn(run func() string) *MockContainerReport_ImageName_Call {
	_c.Call.Return(run)
	return _c
}

// LatestImageID provides a mock function for the type MockContainerReport
func (_mock *MockContainerReport) LatestImageID() types.ImageID {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for LatestImageID")
	}

	var r0 types.ImageID
	if returnFunc, ok := ret.Get(0).(func() types.ImageID); ok {
		r0 = returnFunc()
	} else {
		r0 = ret.Get(0).(types.ImageID)
	}
	return r0
}

// MockContainerReport_LatestImageID_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'LatestImageID'
type MockContainerReport_LatestImageID_Call struct {
	*mock.Call
}

// LatestImageID is a helper method to define mock.On call
func (_e *MockContainerReport_Expecter) LatestImageID() *MockContainerReport_LatestImageID_Call {
	return &MockContainerReport_LatestImageID_Call{Call: _e.mock.On("LatestImageID")}
}

func (_c *MockContainerReport_LatestImageID_Call) Run(run func()) *MockContainerReport_LatestImageID_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockContainerReport_LatestImageID_Call) Return(imageID types.ImageID) *MockContainerReport_LatestImageID_Call {
	_c.Call.Return(imageID)
	return _c
}

func (_c *MockContainerReport_LatestImageID_Call) RunAndReturn(run func() types.ImageID) *MockContainerReport_LatestImageID_Call {
	_c.Call.Return(run)
	return _c
}

// Name provides a mock function for the type MockContainerReport
func (_mock *MockContainerReport) Name() string {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for Name")
	}

	var r0 string
	if returnFunc, ok := ret.Get(0).(func() string); ok {
		r0 = returnFunc()
	} else {
		r0 = ret.Get(0).(string)
	}
	return r0
}

// MockContainerReport_Name_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Name'
type MockContainerReport_Name_Call struct {
	*mock.Call
}

// Name is a helper method to define mock.On call
func (_e *MockContainerReport_Expecter) Name() *MockContainerReport_Name_Call {
	return &MockContainerReport_Name_Call{Call: _e.mock.On("Name")}
}

func (_c *MockContainerReport_Name_Call) Run(run func()) *MockContainerReport_Name_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockContainerReport_Name_Call) Return(s string) *MockContainerReport_Name_Call {
	_c.Call.Return(s)
	return _c
}

func (_c *MockContainerReport_Name_Call) RunAndReturn(run func() string) *MockContainerReport_Name_Call {
	_c.Call.Return(run)
	return _c
}

// State provides a mock function for the type MockContainerReport
func (_mock *MockContainerReport) State() string {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for State")
	}

	var r0 string
	if returnFunc, ok := ret.Get(0).(func() string); ok {
		r0 = returnFunc()
	} else {
		r0 = ret.Get(0).(string)
	}
	return r0
}

// MockContainerReport_State_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'State'
type MockContainerReport_State_Call struct {
	*mock.Call
}

// State is a helper method to define mock.On call
func (_e *MockContainerReport_Expecter) State() *MockContainerReport_State_Call {
	return &MockContainerReport_State_Call{Call: _e.mock.On("State")}
}

func (_c *MockContainerReport_State_Call) Run(run func()) *MockContainerReport_State_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockContainerReport_State_Call) Return(s string) *MockContainerReport_State_Call {
	_c.Call.Return(s)
	return _c
}

func (_c *MockContainerReport_State_Call) RunAndReturn(run func() string) *MockContainerReport_State_Call {
	_c.Call.Return(run)
	return _c
}
