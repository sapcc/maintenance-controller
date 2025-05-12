# SPDX-FileCopyrightText: 2024 SAP SE
# SPDX-License-Identifier: Apache-2.0

{ pkgs ? import <nixpkgs> { } }:

with pkgs;

mkShell {
  nativeBuildInputs = [
    addlicense
    ginkgo
    go-licence-detector
    go_1_24
    golangci-lint
    goreleaser
    gotools # goimports
    kubernetes-controller-tools # controller-gen
    setup-envtest
    # keep this line if you use bash
    bashInteractive
  ];
}
