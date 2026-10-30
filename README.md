<!--
SPDX-FileCopyrightText: 2023 SUSE LLC

SPDX-License-Identifier: Apache-2.0
-->

[![REUSE status](https://api.reuse.software/badge/git.fsfe.org/reuse/api)](https://api.reuse.software/info/git.fsfe.org/reuse/api)

# Inter Server Sync (ISS)

[![Test](https://github.com/uyuni-project/inter-server-sync/actions/workflows/github-actions-tests.yml/badge.svg)](https://github.com/uyuni-project/inter-server-sync/actions/workflows/github-actions-tests.yml)

## Usage
run the command for more information:
`inter-server-sync -h`

## Known limitations 
- Source and target servers need to be on the same version.
- Export and import organization should have the same name.
- Export folder needs to be sync by hand to the target server.

### on source server
- **Create export dir**: `mkdir ~/export`
- **Run command**: `inter-server-sync export --serverConfig=/etc/rhn/rhn.conf --outputDir=~/export --channels=channel_label,channel_label`
- **Copy export directory to target server**: `rsync -r ~/export root@<Target_server>:~/`

### Repository-based export (export-ng)

`export` retains the traditional SQL export of channel, package and errata data,
the original package directory layout, and import-time cache/metadata regeneration SQL.

Use `inter-server-sync export-ng --serverConfig=/etc/rhn/rhn.conf --outputDir=~/export-ng --channels=channel_label,channel_label`
to use the new repository-based implementation instead. It exports channel metadata
and copies repository metadata and packages into `repos/<channel>/repodata` and
`repos/<channel>/getPackage`. Package and errata SQL data and cache-regeneration SQL
are omitted by this implementation; repositories are synchronized during import.

The commands have independent flags and option values. Common flags retain their
existing names and defaults. `--repoBaseExporter` is accepted only by `export-ng`;
it is retained from the new implementation but currently does not change its behavior.
`--metadataOnly` skips repository metadata and package file copying in `export-ng`.
`--noChangelogs` is retained for CLI compatibility on `export-ng`, which does not
export package changelog tables. Use an empty output directory for either command.

### on target server
- **Run command: `inter-server-sync import --importDir ~/export/`

## Database connection configuration

Database connection configuration are loaded by default from `/etc/rhn/rhn.conf`.
File location can be overwritten.
For development environments one can use a sample file in this project.

Steps to run in locally in development mode:
1. copy sample file `cp rhn.conf.exaple rhn.conf`
2. fill all properties in `rhn.conf` with the appropriated values
3. use this configuration file by specifying the config parameter: `go run . -config=rhn.conf`

## Extra

### Dot graph with schema metadata

`go run . dot --serverConfig=rhn.conf |  dot -Tx11`

## Build and release

### 1. Create tag

- Install `uyuni-releng-tools` from [systemsmanagement:Uyuni:Utils](https://build.opensuse.org/project/show/systemsmanagement:Uyuni:Utils)
- Create a tag with the version number using `tito` and push it to github
```
tito tag --use-release=0
git push origin inter-server-sync-x.y.z-1
```

### 2. Create a github release (optional)

- On github create a new version release based on the previous tag

### 3. OBS: project preparation

- Projects names:
    - Uyuni: `systemsmanagement:Uyuni:Master`
    - Head: Devel: `Galaxy:Manager:Head`
    - Manager 4.3: `Devel:Galaxy:Manager:4.3`
- Package name: `inter-server-sync`

In the checked out git repo:

```
export OSCAPI=https://api.opensuse.org
osc -A https://api.opensuse.org branch systemsmanagement:Uyuni:Master inter-server-sync
export OBS_PROJ=home:<your_nick>:branches:systemsmanagement:Uyuni:Master
build-packages-for-obs && push-packages-to-obs
```

### 4. OBS: create submit requests

Uyuni: `osc -A https://api.opensuse.org  sr --no-cleanup <your_project> inter-server-sync systemsmanagement:Uyuni:Master`

Manager Head: `osc -A https://api.suse.de sr --no-cleanup openSUSE.org:<your_project> inter-server-sync Devel:Galaxy:Manager:Head`

For each maintained SUSE Manager version, one SR in the form: `iosc sr --no-cleanup openSUSE.org:<your_project> inter-server-sync Devel:Galaxy:Manager:X.Y`
