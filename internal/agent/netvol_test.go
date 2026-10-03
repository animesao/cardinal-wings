package agent

import (
	"reflect"
	"testing"
)

func TestNetworkCreateArgs(t *testing.T) {
	got := networkCreateArgs("mynet", "10.10.0.0/24")
	want := []string{"network", "create", "--subnet", "10.10.0.0/24", "mynet"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("networkCreateArgs = %v, want %v", got, want)
	}
	got = networkCreateArgs("mynet", "")
	want = []string{"network", "create", "mynet"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("networkCreateArgs no subnet = %v, want %v", got, want)
	}
}

func TestVolumeCreateArgs(t *testing.T) {
	got := volumeCreateArgs("data", "local", map[string]string{"app": "web"})
	want := []string{"volume", "create", "-d", "local", "-l", "app=web", "data"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("volumeCreateArgs = %v, want %v", got, want)
	}
}
