# Google API

Gemini API key via Terraform
Terraform that links billing and creates a restricted Gemini API key in the existing project `YOUR_PROJECT_ID`.

The Terraform files live in your own infrastructure repository; this page is the runbook. Replace `YOUR_PROJECT_ID`, `YOUR_ACCOUNT_EMAIL` and `YOUR_BILLING_ACCOUNT_NAME` with your values.

## What Terraform manages

| File | Resources |
|---|---|
| `main.tf` | `locals` (including `project_id`) |
| `policy.tf` | Enables `apikeys` and `generativelanguage`, creates the API key |
| `billing.tf` | Looks up the billing account, links it to the project |
| `outputs.tf` | `api_key_id`, `api_key` (sensitive), `billing_account` |

The key is restricted to `generativelanguage.googleapis.com`.

## What stays manual

| Item | Why |
|---|---|
| Quota project on ADC | Terraform cannot set your local credentials |
| Enable `serviceusage` and `cloudbilling` APIs | Data sources read at plan time, so these must exist before the first apply |
| IAM roles for your user | Needed before Terraform can run |
| Gemini API prepaid credits | Bought in AI Studio, not available in Terraform |

## Prerequisites

- `gcloud` CLI and `terraform` (1.5 or newer if you use import blocks)
- Account: `YOUR_ACCOUNT_EMAIL`
- An existing project: `YOUR_PROJECT_ID`
- An existing billing account named `YOUR_BILLING_ACCOUNT_NAME`

## 1. Authenticate

```shell
gcloud auth application-default login

# [optional] impersonate a service account instead
gcloud auth application-default login \
  --impersonate-service-account=SA_EMAIL
```

Check which account ADC uses (the `email` field):

```shell
TOKEN=$(gcloud auth application-default print-access-token)
curl -s "https://oauth2.googleapis.com/tokeninfo?access_token=$TOKEN"
```

## 2. Grant roles

| Role | Where | Why |
|---|---|---|
| `roles/serviceusage.serviceUsageConsumer` | Project | Use the project as quota project |
| `roles/billing.viewer` | Billing account | Look up the account by name |
| `roles/billing.user` | Billing account | Link the project to it |

Your user also needs permission to manage services and API keys on the project (for example Owner or Editor).

```shell
gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
  --member="user:YOUR_ACCOUNT_EMAIL" \
  --role="roles/serviceusage.serviceUsageConsumer"
```

The billing roles are granted on the billing account in the console: Billing > Account management > Add principal.

## 3. Set the quota project

ADC has no quota project by default. Without one, Google uses a shared project where APIs are disabled (`SERVICE_DISABLED`).

```shell
gcloud auth application-default set-quota-project YOUR_PROJECT_ID

export USER_PROJECT_OVERRIDE=true
export GOOGLE_BILLING_PROJECT=YOUR_PROJECT_ID
```

## 4. Enable the bootstrap APIs

Terraform needs these before the first apply. Terraform enables `apikeys` and `generativelanguage` itself.

```shell
gcloud services enable \
  serviceusage.googleapis.com \
  cloudbilling.googleapis.com \
  --project YOUR_PROJECT_ID
```

If `serviceusage` is not enabled yet, enable it in the console first:
https://console.developers.google.com/apis/api/serviceusage.googleapis.com/overview?project=YOUR_PROJECT_ID

## 5. Import the existing billing link (optional)

If billing was already linked by hand, import it once so Terraform knows about it:

```shell
terraform init
terraform import google_billing_project_info.this projects/YOUR_PROJECT_ID
```

## 6. Apply

```shell
terraform plan -out=apikey.tfplan
terraform apply "apikey.tfplan"
```

Notes:

- The billing link has `prevent_destroy = true`. Destroying it could unlink billing and break the key.
- The billing account lookup needs an exact, unique `display_name`.

## 7. Buy Gemini prepaid credits

Linking billing is **not enough**. The Gemini API only serves requests with a positive prepaid balance.

1. Open [AI Studio](https://aistudio.google.com) > Billing.
2. Select the project `YOUR_PROJECT_ID`.
3. Buy prepaid credits.
4. Turn on auto-reload.

Notes:

- Google Cloud credits do not apply until you have a positive prepaid balance.
- At $0, all API keys on that billing account stop working at once.

## 8. Get and test the key

```shell
# print the key (sensitive output)
terraform output -raw api_key

# list models with the key
KEY=$(terraform output -raw api_key)
curl -s "https://generativelanguage.googleapis.com/v1beta/models?key=$KEY"
```

A JSON list of models means the key works.

Security notes:

- The key is stored in plain text in the Terraform state. Use a remote backend with restricted access.
- Never commit state files or the key.

## Outputs

| Output | Sensitive | Description |
|---|---|---|
| `api_key_id` | No | Resource ID of the API key |
| `api_key` | Yes | The key string |
| `billing_account` | No | Billing account ID linked to the project |

## Troubleshooting

| Error | Cause | Fix |
|---|---|---|
| 403 `SERVICE_DISABLED` on a Google API | No quota project on ADC | Step 3 |
| 403 `IAM_PERMISSION_DENIED` on billing | Missing billing role | Step 2 |
| Permission error listing projects in the billing lookup | No permission to list projects | Set `lookup_projects = false` in `billing.tf` (already set) |
| `Duplicate data configuration` | Same data source defined twice | Keep it in `billing.tf` only |
| 403 `SERVICE_DISABLED` on the key call | Gemini API not enabled | Re-run apply, check `policy.tf` |
| 403 `API_KEY_SERVICE_BLOCKED` | Key restriction does not match the API called | Add the API to `api_targets` |
| 403 right after key creation | Propagation delay | Wait 2 to 5 minutes |
| 402 Payment Required | No Gemini prepaid balance | Step 7 |
| `xargs: command line cannot be assembled, too long` | Token too long for `xargs` | Use the `TOKEN=$(...)` form in step 1 |

### If key creation is blocked by an org policy

The error will name the constraint. The old override resource was removed because it was not needed. If you ever need it, apply the override with `roles/orgpolicy.policyAdmin` and enable `orgpolicy.googleapis.com`.
