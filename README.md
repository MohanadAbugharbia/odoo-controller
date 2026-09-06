# Odoo-Operator
A Kubernetes operator that manages the full lifecycle of Odoo deployments — including database initialization, secrets, persistent storage, and services — via a single `OdooDeployment` custom resource.

Check out the sample [OdooDeployment](config/samples/odoo_v1_odoodeployment.yaml) for an example of how to use this operator.

Check out the [Odoo](https://www.odoo.com/) website for more information on Odoo.

## Features

| Feature | Status | Description |
|---|---|---|
| Deploy | available | Create and manage Odoo Deployments, Services, and PVCs |
| Module installation | available | Initialize the database and install Odoo modules via a Kubernetes Job |
| Configure Odoo | available | Manage `odoo.conf` settings dynamically via the CR spec (`listDb`, `dbFilter`, `serverWideModules`, `loadLanguages`, `extraOptions`, …) |
| Database provisioning | available | Create the PostgreSQL database when it is missing (`createPolicy`), adopt it when it exists, and drop it on deletion only when the operator created it (`deletionPolicy`) |
| Upgrades | available | Run `-u` maintenance Jobs automatically on image changes or on demand through `spec.upgrade.token`; the Deployment is scaled to zero while the Job runs |
| Safety defaults | available | `list_db = False` + `dbfilter` lockdown, retained filestore and database by default, HTTP probes, resources, env, pod security context, Job deadlines/TTL, CEL-validated spec, `Ready`/`Degraded` conditions and `kubectl get` columns |
| Backup | planned | Snapshot Odoo filestore and database |
| Restore | planned | Restore from a snapshot |

Feel free to request more features by creating an [issue](https://github.com/MohanadAbugharbia/odoo-operator/issues/new?template=Blank+issue)

## Installation

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://github.com/mohanadAbugharbia/odoo-operator/releases/latest/download/install.yaml
```

## Lifecycle

An `OdooDeployment` moves through `status.phase`:

| Phase | Meaning |
|---|---|
| `Pending` | The database connection or the database itself is not settled yet (see the `Degraded` condition for the reason). |
| `Initializing` | The `<name>-init` Job installs `spec.modules` (`-i`, with `--load-language` from `spec.config.loadLanguages`) on `spec.image`. No Deployment exists yet. |
| `Upgrading` | The Deployment is scaled to zero and a `<name>-upgrade-<hash>` Job runs `-i <new modules>` / `-u <spec.upgrade.modules>` on the new image. |
| `Running` | The Deployment runs `status.appliedImage`; `Ready` is True once the requested replicas are available (or `spec.replicas` is 0). |
| `Failed` | A maintenance Job failed. Inspect it with `kubectl logs job/<name>`; deleting the Job retries it. |

Conditions: `Ready`, `DatabaseReady`, `Initialized` and `Degraded` (True only when
something is wrong; its reason names the problem, e.g. `DatabaseMissing`,
`InitJobFailed`, `QuotaExceeded`, `DatabaseDropFailed`).

### Database provenance

- On the first reconcile the operator looks the database up. If it exists it is
  **adopted** (`status.database.provisionedBy: external`). If it is missing and
  `spec.database.createPolicy` is `IfNotExists` (default) the operator creates
  it — `CREATE DATABASE … ENCODING 'unicode' LC_COLLATE 'C' LC_CTYPE 'C' TEMPLATE template0`,
  the same shape Odoo uses — and tags it with the comment
  `odoo-operator:<namespace>/<name>` (`provisionedBy: operator`).
- `spec.database.deletionPolicy: Delete` adds the finalizer
  `odoo.abugharbia.com/database`. On deletion the operator stops the pods and
  drops the database **only if** it recorded `provisionedBy: operator` **and**
  the database comment still equals its tag. A CR pointing at a pre-existing
  database never drops it, whatever the policy says.
- `spec.database.name` (and `nameFromSecret`) are immutable so a rename can
  never orphan or drop the wrong database.
- `spec.odooFilestore.deletionPolicy` works the same way for the filestore PVC
  (Retain by default).

### Upgrades

- `spec.upgrade.onImageChange: true` (default): every `spec.image` change runs
  an upgrade Job with `-u <spec.upgrade.modules>` before the Deployment rolls
  to the new image. New entries in `spec.modules` are installed in the same Job.
- Empty `spec.upgrade.modules` rolls the new image without a Job.
- For production set `onImageChange: false` and bump `spec.upgrade.token` to
  run the upgrade on demand.
- `spec.jobs` bounds the Jobs (`activeDeadlineSeconds`, `ttlSecondsAfterFinished`,
  `backoffLimit`).

### Container command

`spec.odooCommand` (default `["odoo"]`) plus `-c /opt/odoo/odoo.conf` becomes
the container `command`, which **replaces the image ENTRYPOINT**. Images whose
entrypoint script injects database flags (e.g. the official image's
`entrypoint.sh`) are bypassed on purpose: the operator's rendered `odoo.conf`
is the only configuration source. The image's own `addons_path` and
`server_wide_modules` must therefore be re-expressed in
`spec.config.extraAddonsPaths` and `spec.config.serverWideModules`.

## Upgrading to 0.3.0

- **`list_db` is now `False` by default** and `dbfilter = ^<db_name>$` is
  rendered, so an instance only sees its own database. Set
  `spec.config.listDb: true` (and optionally `dbFilter`) to keep the database
  manager.
- `spec.database.ssl` is rendered as `db_sslmode = require` / `disable`
  (previously it was ignored and Odoo used `prefer`).
- `spec.database.name` and `spec.database.nameFromSecret` are immutable.
- `spec.replicas`, `spec.config.workers`, `maxCronThreads`, `withoutDemo` and
  `proxyMode` are pointer fields: `0` and `false` are now honoured.
- Existing CRs roll their pods once (new labels, config-hash annotation,
  probes) and adopt the running image as `status.appliedImage` without a Job.
  Their database is adopted as `external` and is never dropped.
- The `OperatorDegraded`/`OperatorSucceeded` conditions are replaced by
  `Ready`, `DatabaseReady`, `Initialized` and `Degraded`.
- The admin and config Secrets and both Services are now owned by the CR and
  are garbage collected with it.

## Getting Started as a contributor

### Prerequisites
- go version v1.25.0+
- docker version 17.03+.
- kubectl version v1.28.0+.
- Access to a Kubernetes v1.28.0+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/odoo-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/odoo-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Contributing

Please first create an issue with your planned contribution. Anything and everything is welcome.

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

MIT License

Copyright (c) 2026 Mohanad Abugharbia

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

