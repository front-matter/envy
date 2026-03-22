---
description: ""
hideTitle: true
title: worker
toc: false
weight: 6
---

{{< cards cols="1" >}}
{{< card title="worker" cardType="service" dockerImage="ghcr.io/front-matter/invenio-rdm-starter:latest" dockerImageLink="https://ghcr.io/front-matter/invenio-rdm-starter" platform="linux/amd64" command="[ \"celery\", \"-A\", \"invenio_app.celery\", \"worker\", \"--beat\", \"--schedule=/tmp/celerybeat-schedule\", \"--events\", \"--loglevel=WARNING\" ]" tagsSets="base,cache,doi,mail,s3" >}}
{{< /cards >}}

<div id="invenio_accounts_session_redis_url"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_ACCOUNTS_SESSION_REDIS_URL" cardType="var" >}}
{{< /cards >}}

<div id="invenio_admin_email"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_ADMIN_EMAIL" cardType="var" var="info@inveniosoftware.org" >}}
{{< /cards >}}

<div id="invenio_babel_default_locale"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_BABEL_DEFAULT_LOCALE" cardType="var" var="en" >}}
{{< /cards >}}

<div id="invenio_babel_default_timezone"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_BABEL_DEFAULT_TIMEZONE" cardType="var" var="UTC" >}}
{{< /cards >}}

<div id="invenio_cache_redis_url"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_CACHE_REDIS_URL" cardType="var" >}}
{{< /cards >}}

<div id="invenio_celery_broker_url"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_CELERY_BROKER_URL" cardType="var" >}}
{{< /cards >}}

<div id="invenio_celery_result_backend"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_CELERY_RESULT_BACKEND" cardType="var" >}}
{{< /cards >}}

<div id="invenio_datacite_datacenter_symbol"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_DATACITE_DATACENTER_SYMBOL" cardType="var" >}}
{{< /cards >}}

<div id="invenio_datacite_enabled"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_DATACITE_ENABLED" cardType="var" var="false" >}}
{{< /cards >}}

<div id="invenio_datacite_prefix"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_DATACITE_PREFIX" cardType="var" >}}
{{< /cards >}}

<div id="invenio_datacite_test_mode"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_DATACITE_TEST_MODE" cardType="var" var="false" >}}
{{< /cards >}}

<div id="invenio_datacite_username"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_DATACITE_USERNAME" cardType="var" >}}
{{< /cards >}}

<div id="invenio_iso639_languages"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_ISO639_LANGUAGES" cardType="var" var="fr,de,es,pt" >}}
{{< /cards >}}

<div id="invenio_logging_console_level"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_LOGGING_CONSOLE_LEVEL" cardType="var" var="INFO" >}}
{{< /cards >}}

<div id="invenio_mail_password"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_MAIL_PASSWORD" cardType="var" >}}
{{< /cards >}}

<div id="invenio_mail_port"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_MAIL_PORT" cardType="var" var="25" >}}
{{< /cards >}}

<div id="invenio_mail_server"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_MAIL_SERVER" cardType="var" var="127.0.0.1" >}}
{{< /cards >}}

<div id="invenio_mail_suppress_send"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_MAIL_SUPPRESS_SEND" cardType="var" var="true" >}}
{{< /cards >}}

<div id="invenio_mail_use_ssl"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_MAIL_USE_SSL" cardType="var" var="false" >}}
{{< /cards >}}

<div id="invenio_mail_use_tls"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_MAIL_USE_TLS" cardType="var" var="false" >}}
{{< /cards >}}

<div id="invenio_mail_username"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_MAIL_USERNAME" cardType="var" var="info" >}}
{{< /cards >}}

<div id="invenio_ratelimit_storage_url"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_RATELIMIT_STORAGE_URL" cardType="var" >}}
{{< /cards >}}

<div id="invenio_rdm_allow_external_doi_versioning"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_RDM_ALLOW_EXTERNAL_DOI_VERSIONING" cardType="var" var="false" >}}
{{< /cards >}}

<div id="invenio_rdm_allow_metadata_only_records"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_RDM_ALLOW_METADATA_ONLY_RECORDS" cardType="var" var="true" >}}
{{< /cards >}}

<div id="invenio_rdm_allow_restricted_records"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_RDM_ALLOW_RESTRICTED_RECORDS" cardType="var" var="true" >}}
{{< /cards >}}

<div id="invenio_rdm_citation_styles"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_RDM_CITATION_STYLES" cardType="var" var="apa,chicago-author-date,harvard-cite-them-right,ieee,vancouver" >}}
{{< /cards >}}

<div id="invenio_rdm_citation_styles_default"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_RDM_CITATION_STYLES_DEFAULT" cardType="var" >}}
{{< /cards >}}

<div id="invenio_s3_access_key_id"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_S3_ACCESS_KEY_ID" cardType="var" >}}
{{< /cards >}}

<div id="invenio_s3_bucket_name"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_S3_BUCKET_NAME" cardType="var" >}}
{{< /cards >}}

<div id="invenio_s3_endpoint_url"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_S3_ENDPOINT_URL" cardType="var" >}}
{{< /cards >}}

<div id="invenio_s3_region_name"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_S3_REGION_NAME" cardType="var" var="us-east-1" >}}
{{< /cards >}}

<div id="invenio_s3_secret_access_key"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_S3_SECRET_ACCESS_KEY" cardType="var" >}}
{{< /cards >}}

<div id="invenio_search_hosts"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_SEARCH_HOSTS" cardType="var" var="['search:9200']" >}}
{{< /cards >}}

<div id="invenio_search_index_prefix"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_SEARCH_INDEX_PREFIX" cardType="var" var="invenio-rdm-" >}}
{{< /cards >}}

<div id="invenio_security_email_sender"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_SECURITY_EMAIL_SENDER" cardType="var" var="{INVENIO_ADMIN_EMAIL}" >}}
{{< /cards >}}

<div id="test_hardcoded_var"></div>

{{< cards cols="1" >}}
{{< card link="" title="TEST_HARDCODED_VAR" cardType="var" var="envy:readonly:locked-value" >}}
{{< /cards >}}

<div id="test_required_var"></div>

{{< cards cols="1" >}}
{{< card link="" title="TEST_REQUIRED_VAR" cardType="var" var="?required-value" >}}
{{< /cards >}}

<div id="test_secret_var"></div>

{{< cards cols="1" >}}
{{< card link="" title="TEST_SECRET_VAR" cardType="var" >}}
{{< /cards >}}

