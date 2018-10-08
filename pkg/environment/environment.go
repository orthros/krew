// Copyright © 2018 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package environment

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang/glog"
	"k8s.io/client-go/util/homedir"

	"github.com/GoogleContainerTools/krew/pkg/pathutil"
)

// Paths contains all important environment paths
type Paths struct {
	base string
	tmp  string
}

// MustGetKrewPaths returns the inferred paths for krew. By default, it assumes
// $HOME/.krew as the base path, but can be overriden via KREW_ROOT environment
// variable.
func MustGetKrewPaths() Paths {
	base := filepath.Join(homedir.HomeDir(), ".krew")
	if fromEnv := os.Getenv("KREW_ROOT"); fromEnv != "" {
		base = fromEnv
		glog.V(4).Infof("using environment override KREW_ROOT=%s", fromEnv)
	}
	base, err := filepath.Abs(base)
	if err != nil {
		panic(fmt.Errorf("cannot get absolute path, err: %v", err))
	}
	return newPaths(base)
}

func newPaths(base string) Paths {
	return Paths{base: base, tmp: os.TempDir()}
}

// BasePath returns krew base directory.
func (p Paths) BasePath() string { return p.base }

// IndexPath returns the base directory where plugin index repository is cloned.
//
// e.g. {IndexPath}/plugins/{plugin}.yaml
func (p Paths) IndexPath() string { return filepath.Join(p.base, "index") }

// BinPath returns the path where plugin executable symbolic links are found.
// This path should be added to $PATH in client machine.
//
// e.g. {BinPath}/kubectl-foo
func (p Paths) BinPath() string { return filepath.Join(p.base, "bin") }

// DownloadPath returns a temporary directory for downloading plugins. It does
// not create a new directory on each call.
func (p Paths) DownloadPath() string { return filepath.Join(p.tmp, "krew-downloads") }

// InstallPath returns the base directory for plugin installations.
//
// e.g. {InstallPath}/{plugin-name}
func (p Paths) InstallPath() string { return filepath.Join(p.base, "store") }

// PluginInstallPath returns the path to install the plugin.
//
// e.g. {PluginInstallPath}/{version}/{..files..}
func (p Paths) PluginInstallPath(plugin string) string {
	return filepath.Join(p.InstallPath(), plugin)
}

// PluginVersionInstallPath returns the path to the specified version of specified
// plugin.
//
// e.g. {PluginVersionInstallPath} = {PluginInstallPath}/{version}
func (p Paths) PluginVersionInstallPath(plugin, version string) string {
	return filepath.Join(p.InstallPath(), plugin, version)
}

// GetExecutedVersion returns the currently executed version. If krew is
// not executed as an plugin it will return a nil error and an empty string.
func GetExecutedVersion(installPath string, executionPath string, pathResolver func(string) (string, error)) (string, bool, error) {
	path, err := pathResolver(executionPath)
	if err != nil {
		return "", false, fmt.Errorf("failed to resolve path, err: %v", err)
	}

	currentBinaryPath, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}

	pluginsPath, err := filepath.Abs(filepath.Join(installPath, "krew"))
	if err != nil {
		return "", false, err
	}

	elems, ok := pathutil.IsSubPath(pluginsPath, currentBinaryPath)
	if !ok || len(elems) < 2 {
		return "", false, nil
	}

	return elems[0], true, nil
}

// Realpath evaluates symbolic links. If the path is not a symbolic link, it
// returns the cleaned path. Symbolic links with relative paths return error.
func Realpath(path string) (string, error) {
	s, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("failed to stat the currently executed path (%q), err: %v", path, err)
	}

	if s.Mode()&os.ModeSymlink != 0 {
		if path, err = os.Readlink(path); err != nil {
			return "", fmt.Errorf("failed to resolve the symlink of the currently executed version, err: %v", err)
		}
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("symbolic link is relative (%s)", path)
		}
	}
	return filepath.Clean(path), nil
}
