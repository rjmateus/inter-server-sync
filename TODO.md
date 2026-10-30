In the new channels NG exporter:

- we need a new "validateExportFolder":
    - If the folder is empty, all good procede
    - Do not fail if the directory is empty
    - Check the list of channels to export. If a channel is on the export dir in file `exportedChannels.txt` and not part of the export list, abort. We cannot reduce the scope. If a new channel is part of the input export list of channels and not part of the channels in the control file on the file system, that is ok
    - If a new channel is added ask for confirmation before procede
    - If no file is found and the folder is not empty, ask for confirmation


- If the previous check passes
    - remove the files: `exportedChannels.txt`, `hubserver.pem`, `sql_statements.sql.gz`, `sql_statements.sql.gz.sha512` and `version.txt`
    - this files will be recreated by the export

- In the export, channel dumper_ng, there are locations that are hardcoded on the code. They should be read from the configuration files.

- remove unsude parameters for export NG, and re-organize the options object
    - `metadataOnly`
    - `noChangelogs`
    - `packagesOnlyAfter`
    - `repoBaseExporter`

- On the channelDataDuper_NG move the list of tables to variables outside methods

- Package dumper should be separated between NG and old one
    - It should use a rsync, wich should be faster for next exports, since it will not copy if it have not changed