#!/usr/bin/env bash
set -euo pipefail
for name in streaming scenario api json-response faults stateful oauth connector-http llm llm-api github copilot slack discord telegram jira jira-xray stripe notion airtable hubspot gitlab asana trello pagerduty datadog linear twilio sendgrid mailchimp bitbucket circleci paypal dropbox box cloudflare shopify wordpress woocommerce clickup typeform; do
  node scripts/capture-fixture.mjs  "examples/$name.lms" "build/$name.lmcode"
done
mkdir -p build/auth
for name in github-device-flow oauth2-pkce npm-web-login; do
  node scripts/capture-fixture.mjs "examples/auth/$name/server.lms" "build/auth/$name.lmcode"
done
