# Demo workflow: Provision Web Stack

A tangible workflow that provisions a two-tier web stack on the demo containers,
end to end and actually runnable:

```
Configure Database  ──success──▶  Configure Web Server
 (PostgreSQL on db1)               (Apache on web1, web2)
```

It was created in the running stack against the `multinode-demo` inventory
(under the **Operations** org), which holds:

- group **databases** → `db1`
- group **webservers** → `web1`, `web2` (web1 is the runner host)

The two job templates use the inline playbooks here:

- [`postgres.yml`](postgres.yml) — installs PostgreSQL on `databases`, initialises a
  cluster, starts it, creates the `appdb` database and a demo `visits` table.
- [`web.yml`](web.yml) — installs Apache on `webservers`, deploys a landing page,
  starts it, and verifies the page is served.

Launch it from the UI (Workflows → *Provision Web Stack* → Launch) or via the
API. After a run:

- `db1` has PostgreSQL serving `appdb` (table `visits`).
- `web1` / `web2` serve the "Provisioned by Praetor" page.

These playbooks are the source of truth; the job templates store them inline.
