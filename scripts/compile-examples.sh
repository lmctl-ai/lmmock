#!/usr/bin/env bash
set -euo pipefail
for name in streaming scenario api json-response faults stateful oauth connector-http llm llm-api github copilot slack discord telegram jira stripe notion airtable hubspot gitlab asana trello pagerduty datadog linear twilio sendgrid mailchimp bitbucket circleci paypal dropbox box cloudflare shopify wordpress woocommerce clickup typeform; do
  "${LMSCRIPT_BIN:-lmscript}" --no-preflight "examples/$name.lms" "build/$name.dslb"
done
