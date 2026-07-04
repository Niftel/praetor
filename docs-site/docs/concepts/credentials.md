---
sidebar_position: 2
title: Credentials
---

# Credentials

Praetor uses the **AAP machine-credential model**. A credential stores an identity (encrypted at rest) and, at run time, its type's **injectors** render that identity into the environment/files the play uses. There is **no shared platform key** — you own the login accounts and their `authorized_keys` on the hosts.

## Machine credentials

A **Machine** credential holds:

- `username` — the SSH user (`ANSIBLE_REMOTE_USER`),
- `ssh_private_key` — the private key (`ANSIBLE_PRIVATE_KEY_FILE`),
- become settings — `become_method`, `become_username`, `become_password` (`ANSIBLE_BECOME_*`).

Secret fields are encrypted with `PRAETOR_SECRET_KEY` (AES-GCM) and never returned by the API. The operator installs the matching **public** key on hosts (e.g. via `scripts/bootstrap-nodes.sh`); Praetor connects with the private key.

### How it's applied

When the scheduler builds a job manifest it resolves the template's `credential_id` into:

- **env** — `ANSIBLE_REMOTE_USER`, `ANSIBLE_BECOME_METHOD`, `ANSIBLE_BECOME_USER`, …
- **files** — `ANSIBLE_PRIVATE_KEY_FILE`, `ANSIBLE_BECOME_PASSWORD_FILE` (written to temp paths at run time).

Both the executor's SSH bootstrap hop **and** the play's own connections to managed hosts use this identity. A per-host `ansible_user` in the inventory still wins over the credential's username.

:::warning No credential = no remote run
A remote job with no Machine credential (and no per-host `ansible_user`/key) fails at bootstrap with a clear error. This is deliberate — there is no implicit shared key.
:::

## Other credential types

Seeded credential types include **Source Control** (SCM), **Ansible Galaxy/Automation Hub**, and cloud types (**AWS**, **Azure**, **GCP**) for dynamic inventory. Each defines its own inputs + injectors; the same resolve-and-inject machinery renders them (e.g. `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`) for an inventory sync.

## Setting up hosts

`scripts/bootstrap-nodes.sh` provisions the host side the operator owns — creating the automation user, installing the **public** key, and granting passwordless sudo:

```bash
./scripts/bootstrap-nodes.sh -u ansible -a root node1 node2
./scripts/bootstrap-nodes.sh -u ansible --docker web1 web2 db1   # local containers
```

Then create a Machine credential in Praetor with the matching **private** key + `become_method=sudo`, and attach it to your templates.
