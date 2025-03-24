package container

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/nicholas-fedor/watchtower/pkg/types"
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
		wantErr bool
	}{
		{
			name: "SuccessWithValidID",
			setup: func() {
				readFileFunc = func(string) ([]byte, error) {
					return []byte("11:perf_event:/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"), nil
				}
			},
			want:    types.ContainerID("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			wantErr: false,
		},
		{
			name: "FileNotReadable",
			setup: func() {
				readFileFunc = func(string) ([]byte, error) {
					return nil, fmt.Errorf("mocked file read error")
				}
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "NoValidID",
			setup: func() {
				readFileFunc = func(string) ([]byte, error) {
					return []byte("11:perf_event:/user.slice\n10:cpu:/system.slice"), nil
				}
			},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			got, err := GetRunningContainerID()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetRunningContainerID() error = %v, wantErr %v", err, tt.wantErr)
				return
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
		wantErr bool
	}{
		{
			name: "ValidDockerContainerID",
			args: args{
				s: "11:perf_event:/docker/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
			want:    types.ContainerID("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			wantErr: false,
		},
		{
			name: "MultipleLinesWithValidID",
			args: args{
				s: "12:memory:/user.slice\n" +
					"11:perf_event:/docker/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800\n" +
					"10:cpu:/system.slice",
			},
			want:    types.ContainerID("abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567800"),
			wantErr: false,
		},
		{
			name: "NoDockerPattern",
			args: args{
				s: "11:perf_event:/user.slice\n10:cpu:/system.slice",
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "EmptyString",
			args: args{
				s: "",
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "InvalidIDLength",
			args: args{
				s: "11:perf_event:/docker/12345678",
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "NonHexID",
			args: args{
				s: "11:perf_event:/docker/gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg",
			},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getRunningContainerIDFromString(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("getRunningContainerIDFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getRunningContainerIDFromString() = %v, want %v", got, tt.want)
			}
		})
	}
}
