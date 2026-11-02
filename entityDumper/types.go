// SPDX-FileCopyrightText: 2023 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

import (
	"github.com/uyuni-project/inter-server-sync/utils"
)

type DumperOptionsNG struct {
	ServerConfig              string
	ChannelLabels             []string
	ConfigLabels              []string
	ChannelWithChildrenLabels []string
	OutputFolder              string
	outputFolderAbsPath       string
	Containers                bool
	OSImages                  bool
	Orgs                      []uint
	SignKey                   string
	PassFile                  string
}

type DumperOptions struct {
	DumperOptionsNG
	MetadataOnly bool
	NoChangelogs bool
	StartingDate string
}

func (opt *DumperOptionsNG) GetOutputFolderAbsPath() string {
	if "" == opt.outputFolderAbsPath {
		opt.outputFolderAbsPath = utils.GetAbsPath(opt.OutputFolder)
	}
	return opt.outputFolderAbsPath
}

type channelsProcess struct {
	channelsMap map[string]bool
	channels    []string
}

func (c *channelsProcess) addChannelLabel(label string) {
	c.channelsMap[label] = true
	c.channels = append(c.channels, label)
}
