// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package main

import (
	"log"
	"os"

	"github.com/elastic/cloud-on-k8s/v3/hack/release/cmd"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		log.Printf("failed to run command: %s", err)
		os.Exit(1)
	}
}
