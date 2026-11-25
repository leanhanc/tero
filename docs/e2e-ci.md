# End-to-end CI on EC2

Two workflows run the end-to-end tests on throwaway EC2 servers:

- **E2E on EC2** (`.github/workflows/e2e-cloud.yml`) runs the whole suite and the release smoke test on amd64 and arm64, each on Ubuntu 24.04 and 26.04. It runs by hand from the Actions tab and before every release: `release.yml` publishes nothing until it passes.
- **E2E cleanup** (`.github/workflows/e2e-cleanup.yml`) runs every three hours and removes whatever runs that started over four hours ago left behind.

The suite uses a test certificate authority and a fake GitHub, so it can test everything on any server. The smoke test checks what the suite fakes: it installs a release build, runs `sudo tero init`, checks that the dashboard serves a Let's Encrypt certificate browsers trust, and checks the Lynis minimum.

The same scripts run from a laptop with `aws login`:

```bash
scripts/ec2-e2e.sh
```

```bash
scripts/ec2-e2e.sh smoke
```

## Setting it up

GitHub signs in to AWS with its OIDC token, so no AWS key is stored in the repository. The role can only create, tag and remove EC2 resources tagged `tero-e2e`, and only from the repository's `e2e` environment.

1. Confirm the project's Region in AWS Settings > View all projects > Overview > Additional Info > Region. These steps use `us-east-2`.
2. Deploy the role and the budget alert. Leave out `CreateOIDCProvider=false` unless the account already has GitHub's OIDC provider:

   ```bash
   aws cloudformation deploy --profile tero --region us-east-2 --stack-name tero-e2e-ci --template-file infra/e2e-ci.yaml --capabilities CAPABILITY_NAMED_IAM --parameter-overrides BudgetEmail=you@example.com
   ```

3. Read the role's ARN:

   ```bash
   aws cloudformation describe-stacks --profile tero --region us-east-2 --stack-name tero-e2e-ci --query "Stacks[0].Outputs[0].OutputValue" --output text
   ```

4. On GitHub, open Settings > Environments, create an environment named `e2e`, and under Deployment branches and tags allow `main` and tags matching `v*`. Add two environment variables: `TERO_E2E_ROLE_ARN` with the ARN from step 3, and `TERO_E2E_REGION` with `us-east-2`.
5. Run **E2E on EC2** from the Actions tab.

The budget alert watches the whole AWS project's monthly cost, not only e2e servers. A full run uses about 2.5 instance hours of t3.small and t4g.small.

## Proving the Lynis minimum

Run **E2E on EC2** with `skip_step` set to the title of an init step, such as `hardening kernel settings`. The e2e build of Tero then leaves that step out, and the run must fail on the Lynis minimum and the checks for that step.
