In the new channels NG exporter:

- [x] Implement NG export folder validation:
    - Accept missing or empty directories without confirmation.
    - Compare resolved channels, including children, against `exportedChannels.txt`; never allow scope reduction.
    - Ask for confirmation (default No) when adding channels or when a nonempty directory has no control file.
    - Support NG-only `--force` to skip prompts, but not scope checks or filesystem errors.


- [x] After validation and approval, remove generated metadata before recreating it:
    - `exportedChannels.txt`, `hubserver.pem`, `sql_statements.sql.gz`, `sql_statements.sql.gz.sha512`, and `version.txt`.
    - Preserve repository/package data and unrelated files; abort on cleanup errors.

- [x] Read package storage (`mount_point`) for both exporters and package import, and NG metadata cache (`repomd_cache_mount_point`) from the server configuration, retaining existing defaults.

>- remove unsude parameters for export NG, and re-organize the options object
>    - `metadataOnly`
>    - `noChangelogs`
>    - `packagesOnlyAfter`
>    - `repoBaseExporter`

- On the channelDataDuper_NG move the list of tables to variables outside methods

>- Package dumper should be separated between NG and old one
|    - It should use a rsync, wich should be faster for next exports, since it will not copy if it have not changed - NOT IMPLEMENTED FOR NOW