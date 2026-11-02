// SPDX-FileCopyrightText: 2023 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

import (
	"github.com/uyuni-project/inter-server-sync/utils"
)

type DumperOptions struct {
	ServerConfig              string
	ChannelLabels             []string
	ConfigLabels              []string
	ChannelWithChildrenLabels []string
	OutputFolder              string
	outputFolderAbsPath       string
	MetadataOnly              bool
	NoChangelogs              bool
	StartingDate              string
	Containers                bool
	OSImages                  bool
	Orgs                      []uint
	SignKey                   string
	PassFile                  string
	Force                     bool
}

func (opt *DumperOptions) GetOutputFolderAbsPath() string {
	return getOutputFolderAbsPath(&opt.outputFolderAbsPath, opt.OutputFolder)
}

func getOutputFolderAbsPath(absPath *string, outputFolder string) string {
	if *absPath == "" {
		*absPath = utils.GetAbsPath(outputFolder)
	}
	return *absPath
}

type channelsProcess struct {
	channelsMap map[string]bool
	channels    []string
}

func (c *channelsProcess) addChannelLabel(label string) {
	c.channelsMap[label] = true
	c.channels = append(c.channels, label)
}
