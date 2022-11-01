# api

Schema of the Clusterpedia API types that are served by the Kubernetes API server as custom resources.

## Purpose

This library is the canonical location of the Clusterpedia API definition. Most likely interaction with this repository is as a dependency of client-go, controller-runtime or OpenAPI client.

It is published separately to provide clean dependency and public access for repos depending on it.

## Recommended Use

We recommend using the go types in this repo. You may serialize them directly to JSON.

## Where does it come from?

`api` is synced from https://github.com/clusterpedia-io/clusterpedia/blob/{branch}/staging/src/github.com/clusterpedia-io/api. Code changes are made in that location, merged into `github.com/clusterpedia-io/clusterpedia` and later synced here.

## Things you should *NOT* do

1. https://github.com/clusterpedia-io/clusterpedia/blob/{branch}/staging/src/github.com/clusterpedia-io/api is synced to `github.com/clusterpedia-io/clusterpedia`. All changes must be made in the former. The latter is read-only.

## How to generate this repo?

> Note: you need to install [git-filter-repo](https://github.com/newren/git-filter-repo) first (e.g. In macOS, run `brew install git-filter-repo`)

1. git clone https://github.com/clusterpedia-io/clusterpedia.git (always make a fresh copy since it will damage your repo)
2. cd clusterpedia
3. git filter-repo --subdirectory-filter staging/src/github.com/clusterpedia-io/api --force
4. git remote add origin https://github.com/clusterpedia-io/api.git
5. git push origin main
