# Maintenance Controller
![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/sapcc/maintenance-controller/ci.yaml?branch=master)
[![Coverage Status](https://coveralls.io/repos/github/sapcc/maintenance-controller/badge.svg)](https://coveralls.io/github/sapcc/maintenance-controller)
![Docker Pulls](https://img.shields.io/docker/pulls/sapcc/maintenance-controller)

A Kubernetes controller to manage node maintenance.
Serves roughly 50 production clusters across SAP Converged Cloud.

## Installation

Docker Images are on [GitHubs Container registry](https://github.com/sapcc/maintenance-controller/pkgs/container/maintenance-controller) (and older images on [DockerHub](https://hub.docker.com/r/sapcc/maintenance-controller) until they remove them).
A helm chart can be found [here](https://github.com/sapcc/helm-charts/tree/master/system/maintenance-controller).
Alternatively, execute ```make deploy IMG=sapcc/maintenance-controller```.

## Documentation
- [Motivation](docs/motivation.md)
- [Concepts](docs/concepts.md)
- [Configuration](docs/configuration.md)
- [Plugins](docs/plugins.md)
- [Operations](docs/operations.md)
- [Examples](docs/examples.md)

## Additional integrations
- Support for [VMware ESX maintenances](esx/README.md)
- Support for [Kubernikus](kubernikus/README.md)
- Support for [Cluster-API](https://github.com/sapcc/runtime-extension-maintenance-controller)
