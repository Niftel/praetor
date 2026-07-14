# Product

## Register

product

## Platform

web

## Users

Praetor is operated by the whole automation team, not a single role. Platform and SRE engineers own the control plane itself — wiring up templates, inventories, credentials, execution packs, and organizations/teams/users, then watching long-running jobs across fleets. Ansible and automation developers live inside job output and per-task/per-host logs, launching, debugging, and iterating on templates and workflows. Ops and on-call responders arrive under pressure: re-launching failed runs, following event-driven self-healing, and confirming what actually converged after an outage. In practice one team blends these modes — platform owners set the system up and delegate access via RBAC so app teams self-serve their own runs. The interface has to serve the calm configuring hour and the tense incident minute equally well.

## Product Purpose

Praetor is a Kubernetes-native automation platform: a resilient, horizontally scalable, API-compatible alternative to Ansible Tower / AWX / AAP. It runs long-lived playbooks and workflows, survives control-plane database failovers without failing jobs, and gives per-task, per-host observability. The UI is the control plane over that engine — templates, workflows, projects, inventories, credentials, schedules, execution packs, and the org/team/user access model, plus the live job and workflow-run surfaces where execution is watched and debugged. Success is measured on several axes at once: operators **trust** the state they see even mid-outage, daily drivers move quickly from template to running job to root cause, managing many orgs and jobs stays readable at scale, and a team coming from AWX/AAP finds a credible, more polished replacement.

## Positioning

Execution does not depend on the control plane being alive. Jobs keep running through database failovers and outages and always converge to their true outcome, the execution engine is self-contained and pushed to targets rather than installed on them, and the whole system is Kubernetes-native and scales out across execution nodes. Every screen should reinforce that the state shown is the real, converged truth of what happened on the executors — not a hopeful projection.

## Brand Personality

Technical, dense, no-nonsense. This is serious infrastructure tooling for experts, and the voice is direct: precise labels, real data, no marketing gloss or manufactured delight. Density is a feature — information-rich tables, logs, and panels are welcome where the work demands them — but density must never curdle into the clunky, cramped feel of legacy tooling or the soulless flat gray of corporate admin. Character comes from restraint and craft: the deep cobalt control-plane identity, tabular data that aligns, honest status, and a surface that stays legible under load.

## Anti-references

Explicitly not the dated AWX / Ansible Tower UI — the cramped, enterprise-Java feel of the thing being replaced. Not a generic AI-SaaS template: no purple gradients, hero-metric card walls, decorative glassmorphism, or endless identical icon-card grids. Not consumer or playful — no mascots, candy colors, big illustrations, or marketing flourish. And not sterile enterprise gray — no flat clinical white, dead grays, or zero-warmth surfaces. Dense and expert, but crafted and alive.

## Design Principles

Truth over optimism. State must be honest, especially during an outage; the UI shows what actually converged on the executors, never a reassuring guess. Ambiguity about whether a job really succeeded is a failure.

Density without noise. Serve experts with information-rich surfaces, but earn every element — legible hierarchy, tabular alignment, and quiet defaults so density reads as command rather than clutter.

Earned familiarity. This is a tool; it should disappear into the task. Use the standard affordances of the category's best software and don't reinvent buttons, tables, or navigation for flavor. Consistency screen to screen is a virtue.

Replace, don't imitate. Feel like the modern successor to AWX/AAP — as capable and familiar, but more trustworthy and more polished — never a reskin of its clunky ancestor.

Calm under pressure. The same interface must hold up in the configuring hour and the incident minute; motion conveys state, color carries meaning, and nothing shouts unless it must.

## Accessibility & Inclusion

No formal compliance mandate. The bar is sensible, professional readability: honor the reduced-motion and visible-focus treatments already established in the CSS, keep contrast comfortable for long sessions, and keep the app keyboard-usable. Invest where it improves the daily experience of the people who live in the tool, not to satisfy an external checklist.
