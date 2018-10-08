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

package installation

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/GoogleContainerTools/krew/pkg/download"
	"github.com/GoogleContainerTools/krew/pkg/environment"
	"github.com/GoogleContainerTools/krew/pkg/index"
	"github.com/GoogleContainerTools/krew/pkg/pathutil"

	"github.com/golang/glog"
)

// Plugin Lifecycle Errors
var (
	ErrIsAlreadyInstalled = fmt.Errorf("can't install, the newest version is already installed")
	ErrIsNotInstalled     = fmt.Errorf("plugin is not installed")
	ErrIsAlreadyUpgraded  = fmt.Errorf("can't upgrade, the newest version is already installed")
)

const (
	headVersion    = "HEAD"
	headOldVersion = "HEAD-OLD"
	krewPluginName = "krew"
)

func downloadAndMove(version, uri string, fos []index.FileOperation, downloadPath, installPath string) (dst string, err error) {
	glog.V(3).Infof("Creating download dir %q", downloadPath)
	if err = os.MkdirAll(downloadPath, 0755); err != nil {
		return "", fmt.Errorf("could not create download path %q, err: %v", downloadPath, err)
	}
	defer os.RemoveAll(downloadPath)

	if version == headVersion {
		glog.V(1).Infof("Getting latest version from HEAD")
		err = download.GetInsecure(uri, downloadPath, download.HTTPFetcher{})
	} else {
		glog.V(1).Infof("Getting sha256 (%s) signed version", version)
		err = download.GetWithSha256(uri, downloadPath, version, download.HTTPFetcher{})
	}
	if err != nil {
		return "", err
	}

	return moveToInstallDir(downloadPath, installPath, version, fos)
}

// Install will download and install a plugin. The operation tries
// to not get the plugin dir in a bad state if it fails during the process.
func Install(p environment.Paths, plugin index.Plugin, forceHEAD bool) error {
	glog.V(2).Infof("Looking for installed versions")
	_, ok, err := findInstalledPluginVersion(p.InstallPath(), p.BinPath(), plugin.Name)
	if err != nil {
		return err
	}
	if ok {
		return ErrIsAlreadyInstalled
	}

	glog.V(1).Infof("Finding download target for plugin %s", plugin.Name)
	version, uri, fos, bin, err := getDownloadTarget(plugin, forceHEAD)
	if err != nil {
		return err
	}
	return install(plugin.Name, version, uri, bin, p, fos)
}

func install(plugin, version, uri, bin string, p environment.Paths, fos []index.FileOperation) error {
	dst, err := downloadAndMove(version, uri, fos, filepath.Join(p.DownloadPath(), plugin), p.PluginInstallPath(plugin))
	if err != nil {
		return fmt.Errorf("failed to dowload and move during installation, err: %v", err)
	}

	subPathAbs, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("failed to get the absolute fullPath of %q, err: %v", dst, err)
	}
	fullPath := filepath.Join(dst, filepath.FromSlash(bin))
	pathAbs, err := filepath.Abs(fullPath)
	if err != nil {
		return fmt.Errorf("failed to get the absolute fullPath of %q, err: %v", fullPath, err)
	}
	if _, ok := pathutil.IsSubPath(subPathAbs, pathAbs); !ok {
		return fmt.Errorf("the fullPath %q does not extend the sub-fullPath %q", fullPath, dst)
	}
	return createOrUpdateLink(p.BinPath(), filepath.Join(dst, filepath.FromSlash(bin)), plugin)
}

// Remove will remove a plugin.
func Remove(p environment.Paths, name string) error {
	if name == krewPluginName {
		return fmt.Errorf("removing krew is not allowed through krew, see docs for help")
	}
	glog.V(3).Infof("Finding installed version to delete")
	version, installed, err := findInstalledPluginVersion(p.InstallPath(), p.BinPath(), name)
	if err != nil {
		return fmt.Errorf("can't remove plugin, err: %v", err)
	}
	if !installed {
		return ErrIsNotInstalled
	}
	glog.V(1).Infof("Deleting plugin version %s", version)
	glog.V(3).Infof("Deleting path %q", p.PluginInstallPath(name))

	symlinkPath := filepath.Join(p.BinPath(), pluginNameToBin(name, isWindows()))
	if err := removeLink(symlinkPath); err != nil {
		return fmt.Errorf("could not uninstall symlink of plugin: %+v", err)
	}
	return os.RemoveAll(p.PluginInstallPath(name))
}

func createOrUpdateLink(binDir string, binary string, plugin string) error {
	dst := filepath.Join(binDir, pluginNameToBin(plugin, isWindows()))

	if err := removeLink(dst); err != nil {
		return fmt.Errorf("failed to remove old symlink, err: %v", err)
	}
	if _, err := os.Stat(binary); os.IsNotExist(err) {
		return fmt.Errorf("can't create symbolic link, source binary (%q) cannot be found in extracted archive", binary)
	}

	// Create new
	glog.V(2).Infof("Creating symlink from %q to %q", binary, dst)
	if err := os.Symlink(binary, dst); err != nil {
		return fmt.Errorf("failed to create a symlink form %q to %q, err: %v", binDir, dst, err)
	}
	glog.V(2).Infof("Created symlink at %q", dst)

	return nil
}

// removeLink removes a symlink reference if exists.
func removeLink(path string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		glog.V(3).Infof("No file found at %q", path)
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to read the symlink in %q, err: %v", path, err)
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("file %q is not a symlink (mode=%s)", path, fi.Mode())
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove the symlink in %q, err: %v", path, err)
	}
	glog.V(3).Infof("Removed symlink from %q", path)
	return nil
}

func isWindows() bool { return runtime.GOOS == "windows" }

// pluginNameToBin creates the name of the symlink file for the plugin name.
// It converts dashes to underscores.
func pluginNameToBin(name string, isWindows bool) string {
	name = strings.Replace(name, "-", "_", -1)
	name = "kubectl-" + name
	if isWindows {
		name = name + ".exe"
	}
	return name
}
