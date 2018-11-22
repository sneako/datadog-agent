// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2018 Datadog, Inc.

// +build !windows

package flare

import (
	"archive/zip"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DataDog/datadog-agent/cmd/agent/common"
	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/stretchr/testify/assert"
)

// Ensure the permissions.log is being created
func TestPermsFile(t *testing.T) {
	assert := assert.New(t)

	common.SetupConfig("./test")
	config.Datadog.Set("confd_path", "./test/confd")
	config.Datadog.Set("log_file", "./test/logs/agent.log")
	zipFilePath := getArchivePath()
	filePath, err := createArchive(zipFilePath, true, SearchPaths{}, "")
	defer os.Remove(zipFilePath)

	assert.Nil(err)
	assert.Equal(zipFilePath, filePath)

	// asserts that it as indeed created a permissions.log file
	z, err := zip.OpenReader(zipFilePath)
	assert.NoError(err, "opening the zip shouldn't pop an error")

	ok := false
	for _, f := range z.File {
		if strings.HasSuffix(f.Name, "permissions.log") {
			ok = true
		}
	}
	assert.True(ok, "a permissions.log should have been appended to the zip")
}

func TestAddPermsInfo(t *testing.T) {
	assert := assert.New(t)

	err := initPermsInfo(os.TempDir(), "", os.ModePerm)
	assert.NoError(err, "creating the permissions.log info file shouldn't failed")

	// create two files for which we'll add infos into the permissions.log
	f1, err := ioutil.TempFile("", "ddtests*")
	assert.NoError(err, "creating a temporary file should not fail")
	assert.NotNil(f1, "temporary file should not be nil")
	f2, err := ioutil.TempFile("", "ddtests*")
	assert.NoError(err, "creating a temporary file should not fail")
	assert.NotNil(f2, "temporary file should not be nil")

	err = addPermsInfo(os.TempDir(), "", os.ModePerm, f1.Name())
	assert.NoError(err, "addPermsInfos should correctly add the f1 permission to a permissions.log file")
	err = addPermsInfo(os.TempDir(), "", os.ModePerm, f2.Name())

	assert.NoError(err, "addPermsInfos should correctly add the f2 permission to a permissions.log file")

	permsFilePath := filepath.Join(os.TempDir(), "permissions.log")

	// should have created a permissions.log in the tmp dir
	// + added headers and info of the previously created files
	data, err := ioutil.ReadFile(permsFilePath)
	assert.NoError(err, "should be able to read the temporary permissions file")
	assert.Equal(4, strings.Count(string(data), "\n"), "the permissions file should contain 2 lines of headers, 2 lines of entries")

	os.Remove(filepath.Join(os.TempDir(), "permissions.log"))
	os.Remove(f1.Name())
	os.Remove(f2.Name())
}