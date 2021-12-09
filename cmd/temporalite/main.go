// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2021 Datadog, Inc.

package main

import (
	"fmt"
	goLog "log"
	"os"

	"github.com/urfave/cli/v2"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/server/common/headers"
	"go.temporal.io/server/common/log"
	"go.temporal.io/server/temporal"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	// Load sqlite storage driver
	_ "go.temporal.io/server/common/persistence/sql/sqlplugin/sqlite"

	"github.com/DataDog/temporalite"
	"github.com/DataDog/temporalite/internal/liteconfig"
)

var (
	defaultCfg *liteconfig.Config
)

const (
	searchAttrType = "search-attributes-type"
	searchAttrKey  = "search-attributes-key"
	ephemeralFlag  = "ephemeral"
	dbPathFlag     = "filename"
	portFlag       = "port"
	logFormatFlag  = "log-format"
	namespaceFlag  = "namespace"
)

func init() {
	defaultCfg, _ = liteconfig.NewDefaultConfig()
}

func main() {
	if err := buildCLI().Run(os.Args); err != nil {
		goLog.Fatal(err)
	}
}

func buildCLI() *cli.App {
	app := cli.NewApp()
	app.Name = "temporal"
	app.Usage = "Temporal server"
	app.Version = headers.ServerVersion

	app.Commands = []*cli.Command{
		{
			Name:      "start",
			Usage:     "Start Temporal server",
			ArgsUsage: " ",
			Flags: []cli.Flag{
				&cli.StringSliceFlag{
					Name:  searchAttrKey,
					Usage: "Optional search attributes keys that will be registered at startup. If there are multiple keys, concatenate them and separate by ,",
				},
				&cli.StringSliceFlag{
					Name:  searchAttrType,
					Usage: "Optional search attributes types that will be registered at startup. If there are multiple keys, concatenate them and separate by ,",
				},
				&cli.BoolFlag{
					Name:  ephemeralFlag,
					Value: defaultCfg.Ephemeral,
					Usage: "enable the in-memory storage driver **data will be lost on restart**",
				},
				&cli.StringFlag{
					Name:    dbPathFlag,
					Aliases: []string{"f"},
					Value:   defaultCfg.DatabaseFilePath,
					Usage:   "file in which to persist Temporal state",
				},
				&cli.IntFlag{
					Name:    portFlag,
					Aliases: []string{"p"},
					Usage:   "port for the temporal-frontend GRPC service",
					Value:   liteconfig.DefaultFrontendPort,
				},
				&cli.StringFlag{
					Name:    logFormatFlag,
					Usage:   `customize the log formatting (allowed: "json", "pretty")`,
					EnvVars: nil,
					Value:   "json",
				},
				&cli.StringSliceFlag{
					Name:    namespaceFlag,
					Aliases: []string{"n"},
					Usage:   `specify namespaces that should be pre-created`,
					EnvVars: nil,
					Value:   nil,
				},
			},
			Before: func(c *cli.Context) error {
				if c.Args().Len() > 0 {
					return cli.Exit("ERROR: start command doesn't support arguments.", 1)
				}
				if c.IsSet(ephemeralFlag) && c.IsSet(dbPathFlag) {
					return cli.Exit(fmt.Sprintf("ERROR: only one of %q or %q flags may be passed at a time", ephemeralFlag, dbPathFlag), 1)
				}
				if err := searchAttributesValid(c); err != nil {
					return err
				}
				switch c.String(logFormatFlag) {
				case "json", "pretty":
				default:
					return cli.Exit(fmt.Sprintf("bad value %q passed for flag %q", c.String(logFormatFlag), logFormatFlag), 1)
				}
				return nil
			},
			Action: func(c *cli.Context) error {
				opts := []temporalite.ServerOption{
					temporalite.WithFrontendPort(c.Int(portFlag)),
					temporalite.WithDatabaseFilePath(c.String(dbPathFlag)),
					temporalite.WithNamespaces(c.StringSlice(namespaceFlag)...),
					temporalite.WithUpstreamOptions(
						temporal.InterruptOn(temporal.InterruptCh()),
					),
				}
				if c.Bool(ephemeralFlag) {
					opts = append(opts, temporalite.WithPersistenceDisabled())
				}
				if c.IsSet(searchAttrType) && c.IsSet(searchAttrKey) {
					sa, err := parseSearchAttributes(c.StringSlice(searchAttrKey), c.StringSlice(searchAttrType))
					if err != nil {
						return err
					}
					opts = append(opts, temporalite.WithSearchAttributes(sa))
				}
				if c.String(logFormatFlag) == "pretty" {
					lcfg := zap.NewDevelopmentConfig()
					lcfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
					l, err := lcfg.Build(
						zap.WithCaller(false),
						zap.AddStacktrace(zapcore.ErrorLevel),
					)
					if err != nil {
						return err
					}
					logger := log.NewZapLogger(l)
					opts = append(opts, temporalite.WithLogger(logger))
				}

				s, err := temporalite.NewServer(opts...)
				if err != nil {
					return err
				}

				if err := s.Start(); err != nil {
					return cli.Exit(fmt.Sprintf("Unable to start server. Error: %v", err), 1)
				}
				return cli.Exit("All services are stopped.", 0)
			},
		},
	}

	return app
}

func parseSearchAttributes(keys []string, types []string) (map[string]enums.IndexedValueType, error) {
	var searchAttributes = make(map[string]enums.IndexedValueType, len(keys))
	for i, key := range keys {
		t, ok := enums.IndexedValueType_value[types[i]]
		if !ok {
			return nil, fmt.Errorf("the type: %s is not a valid type for a search attribute", types[i])
		}
		searchAttributes[key] = enums.IndexedValueType(t)
	}
	return searchAttributes, nil
}

func searchAttributesValid(c *cli.Context) error {
	if (c.IsSet(searchAttrType) || c.IsSet(searchAttrKey)) && !(c.IsSet(searchAttrType) && c.IsSet(searchAttrKey)) {
		return cli.Exit(fmt.Sprintf("ERROR: both %q and %q must be set at the same time, or omitted completely", searchAttrType, searchAttrKey), 1)
	}
	if c.IsSet(searchAttrType) && c.IsSet(searchAttrKey) && len(c.StringSlice(searchAttrType)) != len(c.StringSlice(searchAttrKey)) {
		return cli.Exit(fmt.Sprintf("ERROR: number of search attributes (type/key) in %q and %q must be the same", searchAttrType, searchAttrKey), 1)
	}
	return nil
}
