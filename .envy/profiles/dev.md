---
hideTitle: true
title: dev
toc: false
weight: 2
---

{{< cards cols="2" >}}
{{< card title="db" link="/services/db" cardType="service" description=`Describes db service configuration.` dockerImage="postgres:17.4-bookworm" dockerImageLink="https://hub.docker.com/_/postgres" tagsSets="db" tagsProfiles="dev,staging" >}}
{{< card title="proxy" link="/services/proxy" cardType="service" description=`Describes proxy service configuration.` dockerImage="caddy:2.10" dockerImageLink="https://hub.docker.com/_/caddy" tagsProfiles="dev,staging" >}}
{{< /cards >}}
