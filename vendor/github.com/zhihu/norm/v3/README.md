# norm

An ORM library support nGQL for Golang.

[![go report card](https://goreportcard.com/badge/github.com/zhihu/norm "go report card")](https://goreportcard.com/report/github.com/zhihu/norm)
[![Go](https://github.com/zhihu/norm/actions/workflows/go.yml/badge.svg)](https://github.com/zhihu/norm/actions/workflows/go.yml)
[![MIT license](https://img.shields.io/badge/license-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)
[![Go.Dev reference](https://img.shields.io/badge/go.dev-reference-blue?logo=go&logoColor=white)](https://pkg.go.dev/github.com/zhihu/norm)

## Overview

* Build insert nGQL by struct / map (Support vertex, edge).
* Parse Nebula execute result to struct / map.
* Easy to use.
* Easy mock for Unit Testing.

**Roadmap**

1. Session pool. For details, please see [dialector](/docs/dialector.adoc)
2. Support more types in insert/execute function.
    * Types: time.Time
3. Support batch insert, query list.
4. Chainable api. For detail please see [chainable api](/docs/chainable_api.adoc)

**Maybe Support**

- [ ] Statistic Hooks. Insert/Query count and latency.
- [ ] Fix fields Order when build insert nGQL. (now norm use map store keys, and in go range map is out-of-order.)

**Need improve**

- [ ] Benchmark.
- [ ] Unit Testing.
- [ ] Documents.

## Getting Started

Install:

```
go get github.com/zhihu/norm
```

use example: please go [use example](/examples/main.go)

## Contributing guidelines

* [code of conduct](/CODE_OF_CONDUCT.md)
* [行为规范 中文版](/CODE_OF_CONDUCT_CN.md)

## License

© Zhihu, 2021~time.Now

Released under the [MIT License](/LICENSE)

_copy and paste from gorm_
