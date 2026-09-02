// Package migrations embute os arquivos SQL de migração forward-only para que o
// runner em internal/platform/migrate possa aplicá-los sem dependência de
// sistema de arquivos. Os próprios arquivos .sql são a fonte da verdade; este
// arquivo apenas os expõe.
package migrations

import "embed"

// FS contém toda migração up/down, nomeada <versão>_<título>.<up|down>.sql
// conforme a convenção do golang-migrate.
//
//go:embed *.up.sql *.down.sql
var FS embed.FS
