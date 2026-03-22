---
description: ""
hideTitle: true
title: cache
toc: false
weight: 1
---

{{< cards cols="1" >}}
{{< card title="cache" cardType="service" dockerImage="valkey/valkey:7.2.5-bookworm" dockerImageLink="https://hub.docker.com/r/valkey/valkey" command="[ \"valkey-server\", \"--loglevel\", \"warning\" ]" >}}
{{< /cards >}}

{{< callout type="info" >}}
This service has no defined env variables.
{{< /callout >}}
