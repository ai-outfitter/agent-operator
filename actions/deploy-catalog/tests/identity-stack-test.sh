#!/usr/bin/env bash
# Structural contract tests for the CloudFormation identity stack. These tests
# need neither AWS credentials nor a live cluster.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
template="$here/../aws/identity-stack.yaml"

passed=0
failed=0

contains() {
  local label="$1" literal="$2"
  if grep -Fq -- "$literal" "$template"; then
    printf 'ok    %s\n' "$label"
    passed=$((passed + 1))
  else
    printf 'FAIL  %s (missing %q)\n' "$label" "$literal"
    failed=$((failed + 1))
  fi
}

excludes() {
  local label="$1" pattern="$2"
  if grep -Eq -- "$pattern" "$template"; then
    printf 'FAIL  %s (matched %q)\n' "$label" "$pattern"
    failed=$((failed + 1))
  else
    printf 'ok    %s\n' "$label"
    passed=$((passed + 1))
  fi
}

contains "template declares an IAM role" "Type: AWS::IAM::Role"
contains "role name is catalog and environment scoped" \
  'RoleName: !Sub "${OrganizationSlug}-catalog-deploy-${Environment}"'
contains "only nonprod and prod are accepted" "AllowedValues:"
contains "nonprod is accepted" "- nonprod"
contains "prod is accepted" "- prod"
contains "the existing OIDC provider is a parameter" "Federated: !Ref OidcProviderArn"
contains "web identity is the only trust action" "Action: sts:AssumeRoleWithWebIdentity"
contains "the AWS STS audience is exact" "token.actions.githubusercontent.com:aud: sts.amazonaws.com"
contains "the GitHub subject is repository and ref exact" \
  'repo:${GitHubOrganization}/${GitHubRepository}:ref:${GitHubRef}'
contains "the role can describe the cluster" "Action: eks:DescribeCluster"
contains "cluster permission is scoped to the exact ARN" \
  'arn:${AWS::Partition}:eks:${AWS::Region}:${AWS::AccountId}:cluster/${ClusterName}'
contains "the role ARN is output" "Value: !GetAtt CatalogDeployRole.Arn"

# One identity string is deliberately reused as IAM role name, aws-auth
# username, and Kubernetes group. All three Ref occurrences plus RoleName make
# drift visible without interpreting CloudFormation tags.
role_refs="$(grep -Fc 'Value: !Ref CatalogDeployRole' "$template")"
if [[ "$role_refs" == 3 ]]; then
  printf 'ok    role name, username, and group share one value\n'
  passed=$((passed + 1))
else
  printf 'FAIL  expected 3 role-name outputs, found %s\n' "$role_refs"
  failed=$((failed + 1))
fi

excludes "the template does not create an OIDC provider" "AWS::IAM::OIDCProvider"
excludes "the template does not use EKS Access Entries" "AWS::EKS::AccessEntry|AccessEntry"
excludes "the role has no wildcard Action or Resource" '(^|[[:space:]])(Action|Resource): ["'\'']?\*'

printf '\n%s passed, %s failed\n' "$passed" "$failed"
(( failed == 0 ))
