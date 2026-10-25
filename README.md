# krobar

krobar is an interim CLI for [kro](https://github.com/kubernetes-sigs/kro)
(Kube Resource Orchestrator). It re-implements the functionality of the
upstream `cmd/kro` CLI so developers and administrators can validate
ResourceGraphDefinitions (RGDs) and generate CRDs, instance manifests, and
resource-graph diagrams from the terminal.

It builds against the published `github.com/kubernetes-sigs/kro` v0.9.3 module.

> **Note:** graph building works **offline by default** — resource schemas are
> resolved from a bundled copy of the Kubernetes OpenAPI document plus any
> CRDs you supply, so `validate` and `generate` need no cluster access. Pass
> `--from-cluster` to resolve schemas via live API discovery instead (as
> upstream does).

## Install

```sh
go install github.com/Matthew-Beckett/krobar/cmd/krobar@latest
```

or build from source:

```sh
make build   # produces ./bin/krobar
```

## Usage

Global flags:

- `--kubeconfig` — path to kubeconfig (default: standard loading rules,
  i.e. `$KUBECONFIG` or `~/.kube/config`, then in-cluster config). Only used
  with `--from-cluster`.
- `--context` — kubeconfig context override. Only used with `--from-cluster`.
- `--verbose` — debug logging to stderr
- `--version` — print version (includes the bundled Kubernetes schema version)

Flags shared by every graph-building command (`validate *`,
`generate *`, `render`, `test`):

- `--crds PATH...` — files or directories of CRDs used for offline schema
  resolution (repeatable; directories are scanned recursively for
  `*.yaml`/`*.yml`/`*.json`, multi-document files supported). RGD documents
  found here are compiled into their generated CRDs — including chains of
  RGDs that reference each other's instance kinds.
- `--from-cluster` — resolve schemas via the live cluster instead of the
  offline resolver (this is when `--kubeconfig`/`--context` take effect).

Commands:

```sh
# Validate an RGD file (use "-" for stdin)
krobar validate rgd -f rgd.yaml
krobar validate rgd rgd.yaml
cat rgd.yaml | krobar validate rgd -f -

# Validate instance manifest(s) against the generated CRD
krobar validate instance -r rgd.yaml -i instance.yaml

# Compare two RGD revisions for API compatibility
krobar validate compat old-rgd.yaml new-rgd.yaml [-o json|yaml]

# Generate the CRD emitted by kro for an RGD (-o yaml|json)
krobar generate crd -f rgd.yaml -o yaml

# Generate a dummy instance manifest of the generated CRD
krobar generate instance -f rgd.yaml --seed 42 -n my-namespace

# Render an interactive HTML graph diagram to stdout
krobar generate diagram -f rgd.yaml > diagram.html

# Canonicalize YAML formatting (2-space indent; canonical key order for RGDs)
krobar fmt rgd.yaml dir/ ...
krobar fmt --check dir/          # print files needing formatting, exit 2
krobar fmt - < rgd.yaml          # read stdin, write stdout

krobar version
krobar completion bash|zsh|fish|powershell
```

The input file may be given via `-f/--file` or as a single positional
argument; `-` reads from standard input.

### Rendering (`krobar render`)

`render` expands an RGD + instance into the concrete resources kro would
create, evaluated by kro's real `pkg/runtime` — CEL templating, `forEach`,
`includeWhen`, `externalRef`, `readyWhen`, and instance status projection all
run through the same engine the controller uses.

```sh
krobar render -f rgd.yaml -i instance.yaml [--observed objects.yaml ...] \
    [--include-instance] [-o yaml|json] [--crds ...] [--from-cluster]
```

- Exactly one instance document is required (`-i -` reads stdin, so
  `krobar generate instance -f rgd.yaml --seed 1 | krobar render -f rgd.yaml -i -`
  composes).
- stdout: rendered objects in dependency (topological) order — a
  `---`-separated YAML stream, or a `v1 List` for `-o json`.
  `--include-instance` appends the defaulted instance with its computed
  `.status`.
- stderr: a `NODE / STATE / DETAIL` summary table and warnings for observed
  objects that matched no node.
- `--observed` files or directories supply live object state (e.g.
  `kubectl get deploy x -o yaml > observed.yaml`, `kind: List` documents are
  unwrapped). Matching is by apiVersion/kind/namespace/name; the observed
  object is deep-merged over the desired object before `readyWhen`
  expressions and status references are evaluated. Nodes whose dependencies
  are unresolved stay `Pending`; `externalRef` nodes are `Pending` until an
  observed object is supplied.
- Exit 0 even with Pending nodes; 1 on errors.

Because rendering is a client-side simulation, the simulated API server is
`desired + observed overlay`: there is no admission, mutating webhook, or
server-side defaulting applied to rendered child resources, and no
controller-observed conditions beyond what `pkg/runtime` computes.

### Fixture tests (`krobar test`)

`krobar test [PATH...]` discovers *test suites* under the given paths
(default `.`) and runs their cases. A suite is either:

- **explicit**: a directory containing a `krobar-test.yaml` manifest
  (`kind: TestSuite`, apiVersion `krobar.dev/v1alpha1`), or
- **implicit**: a directory with a `tests/` subdirectory and exactly one
  top-level YAML file whose kind is `ResourceGraphDefinition`; the suite
  root is `tests/`, the RGD is that file, and a sibling `crds/` directory
  is picked up automatically.

Suites are never nested inside other suites. A path argument may also be a
manifest file directly.

```yaml
# krobar-test.yaml
apiVersion: krobar.dev/v1alpha1
kind: TestSuite
metadata:
  name: webapp             # defaults to the directory name
  labels: {team: a}
spec:
  rgd: ../webapp-rgd.yaml  # required, relative to the suite root
  crds: [../crds]          # optional, relative; merged with --crds
  cases: ["smoke-*"]       # optional globs over suite-root entry names
  instance: {...}          # default instance (XOR instanceFile)
  instanceFile: ./instance.yaml
  snapshot:                # optional snapshot comparison settings
    mode: strict           # strict | partial | off (default strict)
    ignore: [metadata.annotations]  # field paths stripped before comparing
```

A suite-level `instance.yaml` and `observed/` directory in the suite root
are also picked up as defaults. Instance ambiguity at one level (e.g.
`spec.instance` and `instance.yaml` both present) is a fixture error.

Cases live in the suite root:

- each subdirectory containing `case.yaml`, `instance.yaml`, or
  `observed/` is a case named after the directory (a `case.yaml` may
  override via `metadata.name`);
- each other `*.yaml`/`*.yml` file (excluding `krobar-test.yaml` and
  `instance.yaml`) may hold one or more `TestCase` documents;
  `metadata.name` is required there.

A top-level `observed/` directory is suite input, never a case; the same
holds for a top-level `expected/` directory.

```yaml
# <case>/case.yaml
apiVersion: krobar.dev/v1alpha1
kind: TestCase
spec:
  skip: "reason"             # mark the case skipped
  instance: {...}            # override the suite instance (XOR instanceFile)
  instanceFile: ./inst.yaml  # relative to the case file's directory
  instancePatch: {...}       # RFC 7386 merge patch over the base instance
  observed: [{k8s object}]   # inline observed objects
  expect:
    error:                   # build/validate/render must fail
      contains: "substring"  # and/or regex; both must hold when both set
      regex: "..."
    nodes:
      <nodeID>:
        state: Ready         # Ready | NotReady | Pending | Ignored
        count: 1             # number of rendered objects
        objects:             # element i subset-matched vs object i
          - spec: {replicas: 3}
    status: {...}            # subset match on instance .status
    unmatchedObserved: 0     # observed objects that matched no node
  assert:                    # richer assertions (see below)
    - name: replicas-ok
      node: deployment
      cel: self.spec.replicas == instance.spec.replicas
  snapshot: "off"            # or a snapshot directory path (see below)
```

`assert` entries run after `expect`. Each needs at least one of `state`,
`count`, `match`, `equals`, `absent`, `cel`:

```yaml
- name: container-check      # optional; failures print as assert "container-check":
  node: deployment           # subject node; omit to check the instance object
  index: 0                   # pick object 0 (XOR select, each)
  select: {kind: Deployment, name: myapp}   # pick the unique matching object
  each: true                 # or apply to every object
  path: spec.template.spec.containers[0]    # narrow the subject first
  state: Ready               # node state (requires node)
  count: 1                   # object count (requires node)
  match: {spec: {replicas: 3}}              # subset match
  equals: {...}              # exact match (no extra keys; YAML diff on failure)
  absent: [metadata.annotations]            # paths that must not exist
  cel: self.spec.replicas > 0               # CEL expression over the subject
```

Subjects resolve as: `index` by position, `select` by
name/namespace/kind, `each` per object, or the single object of the node
(exactly one required). `path` uses field selectors
(`a.b[0]`, `metadata.labels["app.kubernetes.io/name"]`) to narrow the
subject before `match`/`equals`/`absent`/`cel`.

CEL sees `self` (the subject), `instance` (the whole instance), `status`
(the computed `.status`), `nodes` (id to `{state, reason, objects}`) and
`vars` (empty). Numeric comparisons are cross-type: YAML-decoded floats
and rendered integers compare equal.

Inside `match` and `equals`, a map whose keys are all `$`-prefixed is an
operator map (`$$` escapes a literal `$` key; operators and plain keys
cannot mix):

| Operator | Meaning |
|----------|---------|
| `$regex: <str>` | string matches RE2 pattern (unanchored) |
| `$any: true` | key exists, any value |
| `$absent: true` | key must not exist (map values only) |
| `$len: <int>` | list/map/string has this length |
| `$contains: [...]` | each element subset-matches a distinct list element |
| `$not: <expected>` | the inner expected value must not match |
| `$cel: <expr>` | CEL expression with `self` bound to the value |

### Snapshots

After `expect` and `assert`, each case can compare the rendered result
against snapshot files. Directory cases use `<case>/expected/`; a case
may instead point `spec.snapshot` at another directory relative to the
case file, or set `snapshot: "off"`. Standalone-file cases have no
conventional directory and opt in with `spec.snapshot: <dir>` only.

The snapshot directory holds:

- `<nodeID>.yaml`: the node's rendered objects as a multi-document YAML
  stream, in render order. Node IDs usable in file names are limited to
  `[A-Za-z0-9-._]` and cannot start with `_`.
- `_states.yaml`: a map of every node ID to its state.
- `_status.yaml`: the instance's computed `.status` (written only when
  non-empty).

Comparison semantics come from the suite's `spec.snapshot.mode`
(default `strict`):

- `strict`: the directory must exist, every node that rendered objects
  needs a file, `_states.yaml` must cover every result node, and a
  non-empty computed status needs `_status.yaml`. Files for nodes absent
  from the result, and unknown `_states.yaml` entries, are failures.
- `partial`: only files present on disk are compared; missing files and
  directories are skipped.
- `off`: no comparison.

Object documents and `.status` compare with exact-match semantics and a
unified diff on mismatch; `$`-operators are not allowed inside snapshot
files. Paths listed in the suite's `spec.snapshot.ignore` are deleted
from rendered objects and `.status` before comparing (and before
writing), which keeps volatile fields such as generated annotations out
of the snapshots.

`krobar test -u` (`--update-snapshots`) rewrites each case's snapshot
directory from the actual result instead of comparing: node files are
written for every rendered node, `_states.yaml` always, `_status.yaml`
when the status is non-empty, and every other `*.yaml`/`*.yml` file in
the directory is removed. Cases that are skipped, filtered out, fixture
errors, or expecting `expect.error` are never touched. `expect` and
`assert` still run, so a case can fail while its snapshots refresh.

The instance resolves case-first: case `instance`/`instanceFile`/
`<case>/instance.yaml`, else the suite default. Observed objects merge
from `<root>/observed/`, `<case>/observed/`, and inline `observed`, keyed
by apiVersion/kind/namespace/name; later sources replace earlier objects
whole. Expectations are subset matches (expected keys must exist and match
recursively; lists compare element-wise; numbers normalise int/float) with
field paths in mismatch messages. Documents are strict-decoded: unknown
fields fail loudly.

Flags: `--run REGEX` (matched against `suite/case` paths), `--junit FILE`
(JUnit XML report), `-u`/`--update-snapshots` (rewrite snapshot files),
plus the graph flags. Output is go-test style; the
suite's graph is built once and shared by its cases. Exit is non-zero on
any failure, when no tests are found, or when `--run` matches nothing. The
old single-file `RGDTest` kind was removed: convert fixtures to TestSuite
directories.

```sh
krobar test ./rgds --junit report.xml   # CI usage
```

Example suites live under `testdata/tests/`.

### Offline schema resolution

krobar compiles in the published Kubernetes OpenAPI v2 document
(`api/openapi-spec/swagger.json` for Kubernetes 1.36 — `krobar version`
reports the bundled version). Built-in kinds resolve from that document;
custom kinds resolve from CRDs supplied via `--crds`. CRD schemas are
augmented with the same `apiVersion`/`kind`/`metadata` (full ObjectMeta)
top-level properties the API server exposes, so offline graph building is
byte-identical to `--from-cluster` output. A `--kubernetes-version` flag
(KREP-021) is intentionally not supported: exactly one schema set is
compiled in.

`validate instance` applies the same checks the API server would:
structural-schema defaulting, pruning of unknown fields (reported as
errors), OpenAPI schema validation, and CEL `x-kubernetes-validations`.

### Exit codes

- `0` — success
- `1` — error or validation failure (invalid RGD/instance, breaking
  compatibility changes)
- `2` — `fmt --check` found files that need formatting

## Intentional differences from upstream `cmd/kro`

- Graph building works offline by default (bundled Kubernetes 1.36 schemas);
  `--from-cluster` restores the upstream online behaviour.
- `--kubeconfig`, `--context`, and `--verbose` flags are actually honoured
  (upstream accepted but ignored them).
- A positional file argument works for `generate` and `validate rgd`; `-`
  reads from stdin.
- CRD/instance YAML marshals via `sigs.k8s.io/yaml`, so `json` struct tags
  are respected (upstream marshalled the CRD with `yaml.v2`, producing wrong
  field names); JSON output is pretty-printed.
- Diagram output is deterministic (nodes/links are emitted in sorted order).
- `generate instance` supports `--seed` for reproducible dummy data and
  `--namespace/-n` for the instance namespace.
- `spec.schema.group`/`scope` default to the API-server values (`kro.run`,
  `Namespaced`) when omitted from the file.
- `generate crd` stamps the CRD's TypeMeta
  (`apiextensions.k8s.io/v1`/`CustomResourceDefinition`), which the upstream
  builder leaves empty, so output can be `kubectl apply`ed directly.
- `generate instance` emits the instance's real `apiVersion`/`kind` from the
  CRD spec (upstream used the CRD object's own TypeMeta, producing empty
  values), and omits `metadata.namespace` for cluster-scoped CRDs.
- `kro.run/cli-version` / `kro.run/version` annotations carry the real krobar
  version instead of the literal `"dev"`, and existing annotations are merged
  rather than replaced.
- The emulator no longer panics on malformed vendor extensions, non-string
  enum values, or inverted min/max constraints.
- Output is written to the command's configured writer (cobra streams), so
  `krobar ... > file` and test capture behave correctly.
- Extra commands: `validate instance`, `validate compat`, `fmt`, `render`,
  `test`.
- `krobar test` goes beyond KREP-021 (which only mentions fixture files):
  it runs directory-based suites with case discovery, instance patches,
  observed-object merging, skip/filter semantics, and JUnit output.

## Development

- `make build` — build `bin/krobar` with version ldflags
- `make test` — run the test suite with `-race`
- `make lint` — run golangci-lint
- `make snapshot` — local goreleaser snapshot build into `dist/`
- `go vet ./...` — vet; `gofmt -l .` must be empty

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE). Portions of
this codebase are derived from upstream kro (Copyright The Kubernetes
Authors).
