package container

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Static error for test.
var (
	ErrMockedFileRead = errors.New("mocked file read error")
)

func TestGetRunningContainerID(t *testing.T) {
	originalReadFileFunc := readFileFunc
	defer func() {
		readFileFunc = originalReadFileFunc
	}()

	tests := []struct {
		name    string
		setup   func()
		want    types.ContainerID
		wantErr error
	}{
		{
			name: "SuccessWithValidID",
			setup: func() {
				readFileFunc = func(string) ([]byte, error) {
					return []byte(
						"11:perf_event:/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					), nil
				}
			},
			want: types.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			),
			wantErr: nil,
		},
		{
			name: "FileNotReadable",
			setup: func() {
				readFileFunc = func(string) ([]byte, error) {
					return nil, ErrMockedFileRead
				}
			},
			want:    "",
			wantErr: errReadCgroupFile,
		},
		{
			name: "NoValidID",
			setup: func() {
				readFileFunc = func(string) ([]byte, error) {
					return []byte("11:perf_event:/user.slice\n10:cpu:/system.slice"), nil
				}
			},
			want:    "",
			wantErr: errExtractContainerID,
		},
		{
			name: "EmptyFileContent",
			setup: func() {
				readFileFunc = func(string) ([]byte, error) {
					return []byte(""), nil
				}
			},
			want:    "",
			wantErr: errExtractContainerID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}

			got, err := GetRunningContainerID()
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("GetRunningContainerID() error = %v, want no error", err)

					return
				}
			} else {
				if err == nil {
					t.Errorf("GetRunningContainerID() expected error %v, got nil", tt.wantErr)

					return
				}

				if !errors.Is(err, tt.wantErr) {
					t.Errorf("GetRunningContainerID() error = %v, want error wrapping %v", err, tt.wantErr)
				}
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetRunningContainerID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getRunningContainerIDFromString(t *testing.T) {
	type args struct {
		s string
	}

	tests := []struct {
		name    string
		args    args
		want    types.ContainerID
		wantErr error
	}{
		{
			name: "ValidDockerContainerIDSingleLine",
			args: args{
				s: "11:perf_event:/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
			want: types.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			),
			wantErr: nil,
		},
		{
			name: "ValidDockerContainerIDMultiLine",
			args: args{
				s: "12:memory:/user.slice\n11:perf_event:/docker/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800\n10:cpu:/system.slice",
			},
			want: types.ContainerID(
				"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800",
			),
			wantErr: nil,
		},
		{
			name: "NoDockerPatternMultiLine",
			args: args{
				s: "11:perf_event:/user.slice\n10:cpu:/system.slice",
			},
			want:    "",
			wantErr: errNoValidContainerID,
		},
		{
			name: "EmptyString",
			args: args{
				s: "",
			},
			want:    "",
			wantErr: errNoValidContainerID,
		},
		{
			name: "InvalidIDLength",
			args: args{
				s: "11:perf_event:/docker/12345678",
			},
			want:    "",
			wantErr: errNoValidContainerID,
		},
		{
			name: "NonHexID",
			args: args{
				s: "11:perf_event:/docker/gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg",
			},
			want:    "",
			wantErr: errNoValidContainerID,
		},
		{
			name: "ValidIDWithExtraLines",
			args: args{
				s: "11:perf_event:/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef\n10:cpu:/system.slice",
			},
			want: types.ContainerID(
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			),
			wantErr: nil,
		},
		{
			name: "NoDockerPatternSingleLine",
			args: args{
				s: "11:perf_event:/user.slice",
			},
			want:    "",
			wantErr: errNoValidContainerID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getRunningContainerIDFromString(tt.args.s)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("getRunningContainerIDFromString() error = %v, want no error", err)

					return
				}
			} else {
				if err == nil {
					t.Errorf("getRunningContainerIDFromString() expected error %v, got nil", tt.wantErr)

					return
				}

				if !errors.Is(err, tt.wantErr) {
					t.Errorf("getRunningContainerIDFromString() error = %v, want error wrapping %v", err, tt.wantErr)
				}
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getRunningContainerIDFromString() = %v, want %v", got, tt.want)
			}
		})
	}
}
