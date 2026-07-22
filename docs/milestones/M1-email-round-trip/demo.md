# M1 Demo: Local Email Round Trip

This is the executable acceptance contract for M1. It proves transport, generic
scheduled Pi execution, skill-owned mail behavior, server-side reply state, and
restart durability. It does not claim that the M2 paper-research workflow runs.

## Demonstrated behavior

The sender `demo-user@link.test` submits one plain-text message to
`researcher@link.test` through Stalwart JMAP. The agent observes the message,
the generic loop wakes the resident Pi session, and the `mail` skill uses `xin`
to submit a threaded reply before moving the original from `INBOX` to
`Processed`. The verifier checks the reply and mailbox state again after
restarting the agent Deployment.

## Inputs

- The repository's devenv shell and local microVM support.
- A running local cluster initialized by `cluster:up` and `operator:install`.
- The developer's `$HOME/.pi` directory. Copying this directory into the agent
  PVC is a required part of the scenario.
- The deterministic local-only Stalwart accounts:
  - `researcher@link.test`
  - `demo-user@link.test`

The demo uses local development credentials declared by the isolated cluster
fixture. It does not send mail to the Internet.

## Run

From the repository root:

```sh
devenv tasks run cluster:up
devenv tasks run operator:install
devenv tasks run demo:m1
```

`demo:m1` MUST perform these steps without manual `kubectl` intervention:

1. Apply the `mail-loop-demo` organization and `researcher` agent.
2. Wait for namespace `agent-researcher`.
3. Create the demo JMAP Secret and runtime ConfigMap in that namespace.
4. Copy the complete local `$HOME/.pi` directory directly into the durable
   agent volume through the temporary `pi-seeder` pod, delete that pod, and only
   then unblock the agent Deployment.
5. Restart the Deployment so the current locally imported image is running.
6. Wait for the initial empty work survey to settle, then submit a
   uniquely-subjected plain-text probe from `demo-user@link.test` to
   `researcher@link.test` and record Stalwart's generated Message-ID.
7. Let a later one-minute scheduled wakeup discover the probe, then authenticate
   to the sender's JMAP account and require exactly one matching
   reply with:

   ```text
   From: M1 researcher agent <researcher@link.test>
   To: demo-user@link.test
   Subject: Re: <probe subject>
   In-Reply-To: <probe Message-ID>
   References: <probe Message-ID>
   ```

   Here `From` is the return address exposed by the delivered JMAP Email. Local
   JMAP delivery does not require an SMTP `Return-Path` header.
8. Require exactly one matching original in `Processed` and none in `INBOX`.
9. Confirm `/workspace/.pi/agent/auth.json` exists and is non-empty when the
   source file exists locally.
10. Restart the Deployment, then prove the original remains outside `INBOX` and
    the sender mailbox still contains exactly one reply.

## Evidence

The task MUST retain these ignored artifacts under
`.devenv/state/link-cluster/shared/evidence/m1-email-flow/`:

- `subject.txt` and `probe-message-id.txt` for the unique probe;
- `initial-loop.jsonl`, proving the startup survey settled before the probe;
- `send.json`, `original-sent.json`, `reply-search.json`, and `reply.json`;
- `inbox-before.json`, `processed-after.json`, and `inbox-after.json`;
- `replies-after-restart.json` and `inbox-after-restart.json`; and
- `agent.yaml` without Secret data.

The reply artifacts MUST include the JMAP id, Message-ID, `In-Reply-To`,
`References`, `From`, `To`, and subject. A failed assertion MUST make the task
non-zero. Secret values and the copied `.pi` payload MUST NOT appear in the
evidence bundle.

## Expected result

The task prints:

```text
M1 email reply verified with exact threading headers; original moved to Processed and was not re-replied after restart: <probe subject>
```

The research-paper, wiki, Git LFS, Docling, model, and final response workflow is
the [M2 demo](../M2-email-paper-research/demo.md).
