---
hideTitle: true
title: staging
toc: false
weight: 3
---

{{< cards cols="2" >}}
{{< card title="db" titleLink="/services/db" cardType="service" description=`Describes db service configuration.` dockerImage="postgres:17.4-bookworm" dockerImageLink="https://hub.docker.com/_/postgres" tagsSets="db" tagsProfiles="dev,staging" >}}
{{< card title="proxy" titleLink="/services/proxy" cardType="service" description=`Describes proxy service configuration.` dockerImage="caddy:2.10" dockerImageLink="https://hub.docker.com/_/caddy" tagsProfiles="dev,staging" >}}
{{< /cards >}}
