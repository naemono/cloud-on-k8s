// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	// RootCmd represents the root command
	RootCmd = &cobra.Command{
		Use:   "release",
		Short: "Release management tool for ECK",
		Long:  `A CLI tool for managing ECK releases.`,
	}
)

func init() {
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.SetEnvPrefix("RELEASE")
	viper.AutomaticEnv()

	RootCmd.AddCommand(NewFeatureFreezeCmd())
}
