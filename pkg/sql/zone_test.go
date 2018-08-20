// Copyright 2017 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package sql_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/config"
	"github.com/cockroachdb/cockroach/pkg/keys"
	"github.com/cockroachdb/cockroach/pkg/sql/lex"
	"github.com/cockroachdb/cockroach/pkg/sql/tests"
	"github.com/cockroachdb/cockroach/pkg/testutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/serverutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/sqlutils"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
)

func TestValidSetShowZones(t *testing.T) {
	defer leaktest.AfterTest(t)()

	params, _ := tests.CreateTestServerParams()
	s, db, _ := serverutils.StartServer(t, params)
	defer s.Stopper().Stop(context.TODO())

	sqlDB := sqlutils.MakeSQLRunner(db)
	sqlDB.Exec(t, `CREATE DATABASE d; USE d; CREATE TABLE t ();`)

	yamlDefault := fmt.Sprintf("gc: {ttlseconds: %d}", config.DefaultZoneConfig().GC.TTLSeconds)
	yamlOverride := "gc: {ttlseconds: 42}"
	zoneOverride := config.DefaultZoneConfig()
	zoneOverride.GC.TTLSeconds = 42

	defaultRow := sqlutils.ZoneRow{
		ID:           keys.RootNamespaceID,
		CLISpecifier: ".default",
		Config:       config.DefaultZoneConfig(),
	}
	defaultOverrideRow := sqlutils.ZoneRow{
		ID:           keys.RootNamespaceID,
		CLISpecifier: ".default",
		Config:       zoneOverride,
	}
	metaRow := sqlutils.ZoneRow{
		ID:           keys.MetaRangesID,
		CLISpecifier: ".meta",
		Config:       zoneOverride,
	}
	systemRow := sqlutils.ZoneRow{
		ID:           keys.SystemDatabaseID,
		CLISpecifier: "system",
		Config:       zoneOverride,
	}
	jobsRow := sqlutils.ZoneRow{
		ID:           keys.JobsTableID,
		CLISpecifier: "system.jobs",
		Config:       zoneOverride,
	}
	dbDescID := uint32(keys.MinNonPredefinedUserDescID)
	dbRow := sqlutils.ZoneRow{
		ID:           dbDescID,
		CLISpecifier: "d",
		Config:       zoneOverride,
	}
	tableRow := sqlutils.ZoneRow{
		ID:           dbDescID + 1,
		CLISpecifier: "d.t",
		Config:       zoneOverride,
	}

	// Remove stock zone configs installed at cluster bootstrap. Otherwise this
	// test breaks whenever these stock zone configs are adjusted.
	sqlutils.RemoveAllZoneConfigs(t, sqlDB)

	// Ensure the default is reported for all zones at first.
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "RANGE default", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "RANGE meta", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE system", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE system.lease", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE d", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE d.t", defaultRow)

	// Ensure a database zone config applies to that database and its tables, and
	// no other zones.
	sqlutils.SetZoneConfig(t, sqlDB, "DATABASE d", yamlOverride)
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultRow, dbRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "RANGE meta", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE system", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE system.lease", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE d", dbRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE d.t", dbRow)

	// Ensure a table zone config applies to that table and no others.
	sqlutils.SetZoneConfig(t, sqlDB, "TABLE d.t", yamlOverride)
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultRow, dbRow, tableRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "RANGE meta", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE system", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE system.lease", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE d", dbRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE d.t", tableRow)

	// Ensure a named zone config applies to that named zone and no others.
	sqlutils.SetZoneConfig(t, sqlDB, "RANGE meta", yamlOverride)
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultRow, metaRow, dbRow, tableRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "RANGE meta", metaRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE system", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE system.lease", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE d", dbRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE d.t", tableRow)

	// Ensure updating the default zone propagates to zones without an override,
	// but not to those with overrides.
	sqlutils.SetZoneConfig(t, sqlDB, "RANGE default", yamlOverride)
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultOverrideRow, metaRow, dbRow, tableRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "RANGE meta", metaRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE system", defaultOverrideRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE system.lease", defaultOverrideRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE d", dbRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE d.t", tableRow)

	// Ensure deleting a database deletes only the database zone, and not the
	// table zone.
	sqlutils.DeleteZoneConfig(t, sqlDB, "DATABASE d")
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultOverrideRow, metaRow, tableRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE d", defaultOverrideRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE d.t", tableRow)

	// Ensure deleting a table zone works.
	sqlutils.DeleteZoneConfig(t, sqlDB, "TABLE d.t")
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultOverrideRow, metaRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE d.t", defaultOverrideRow)

	// Ensure deleting a named zone works.
	sqlutils.DeleteZoneConfig(t, sqlDB, "RANGE meta")
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultOverrideRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "RANGE meta", defaultOverrideRow)

	// Ensure deleting non-overridden zones is not an error.
	sqlutils.DeleteZoneConfig(t, sqlDB, "RANGE meta")
	sqlutils.DeleteZoneConfig(t, sqlDB, "DATABASE d")
	sqlutils.DeleteZoneConfig(t, sqlDB, "TABLE d.t")

	// Ensure updating the default zone config applies to zones that have had
	// overrides added and removed.
	sqlutils.SetZoneConfig(t, sqlDB, "RANGE default", yamlDefault)
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "RANGE default", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "RANGE meta", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE system", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE system.lease", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE d", defaultRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE d.t", defaultRow)

	// Ensure the system database zone can be configured, even though zones on
	// config tables are disallowed.
	sqlutils.SetZoneConfig(t, sqlDB, "DATABASE system", yamlOverride)
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultRow, systemRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "DATABASE system", systemRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE system.namespace", systemRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE system.jobs", systemRow)

	// Ensure zones for non-config tables in the system database can be
	// configured.
	sqlutils.SetZoneConfig(t, sqlDB, "TABLE system.jobs", yamlOverride)
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultRow, systemRow, jobsRow)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE system.jobs", jobsRow)

	// Verify that the session database is respected.
	sqlutils.SetZoneConfig(t, sqlDB, "TABLE t", yamlOverride)
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE t", tableRow)
	sqlutils.DeleteZoneConfig(t, sqlDB, "TABLE t")
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE t", defaultRow)

	// Verify we can use composite values.
	sqlDB.Exec(t, fmt.Sprintf("ALTER TABLE t CONFIGURE ZONE '' || %s || ''",
		lex.EscapeSQLString(yamlOverride)))
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE t", tableRow)

	// Ensure zone configs are read transactionally instead of from the cached
	// system config.
	txn, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	sqlutils.TxnSetZoneConfig(t, sqlDB, txn, "RANGE default", yamlOverride)
	sqlutils.TxnSetZoneConfig(t, sqlDB, txn, "TABLE d.t", "") // this should pick up the overridden default config
	if err := txn.Commit(); err != nil {
		t.Fatal(err)
	}
	sqlutils.VerifyZoneConfigForTarget(t, sqlDB, "TABLE d.t", tableRow)

	sqlDB.Exec(t, "DROP TABLE d.t")
	_, err = sqlDB.DB.Exec("SHOW ZONE CONFIGURATION FOR TABLE d.t")
	if !testutils.IsError(err, `relation "d.t" does not exist`) {
		t.Errorf("expected SHOW ZONE CONFIGURATION to fail on dropped table, but got %q", err)
	}
	sqlutils.VerifyAllZoneConfigs(t, sqlDB, defaultOverrideRow, systemRow, jobsRow)
}

func TestInvalidSetShowZones(t *testing.T) {
	defer leaktest.AfterTest(t)()

	params, _ := tests.CreateTestServerParams()
	s, db, _ := serverutils.StartServer(t, params)
	defer s.Stopper().Stop(context.TODO())

	for i, tc := range []struct {
		query string
		err   string
	}{
		{
			"ALTER RANGE default CONFIGURE ZONE NULL",
			"cannot remove default zone",
		},
		{
			"ALTER RANGE default CONFIGURE ZONE '&!@*@&'",
			"could not parse zone config",
		},
		{
			"ALTER TABLE system.namespace CONFIGURE ZONE ''",
			"cannot set zone configs for system config tables",
		},
		{
			"ALTER RANGE foo CONFIGURE ZONE ''",
			`"foo" is not a built-in zone`,
		},
		{
			"ALTER DATABASE foo CONFIGURE ZONE ''",
			`database "foo" does not exist`,
		},
		{
			"ALTER TABLE system.foo CONFIGURE ZONE ''",
			`relation "system.foo" does not exist`,
		},
		{
			"ALTER TABLE foo CONFIGURE ZONE ''",
			`relation "foo" does not exist`,
		},
		{
			"SHOW ZONE CONFIGURATION FOR RANGE foo",
			`"foo" is not a built-in zone`,
		},
		{
			"SHOW ZONE CONFIGURATION FOR DATABASE foo",
			`database "foo" does not exist`,
		},
		{
			"SHOW ZONE CONFIGURATION FOR TABLE foo",
			`relation "foo" does not exist`,
		},
		{
			"SHOW ZONE CONFIGURATION FOR TABLE system.foo",
			`relation "system.foo" does not exist`,
		},
	} {
		if _, err := db.Exec(tc.query); !testutils.IsError(err, tc.err) {
			t.Errorf("#%d: expected error matching %q, but got %v", i, tc.err, err)
		}
	}
}
