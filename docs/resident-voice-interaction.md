# Resident agent voice interaction

Status: proposed product direction, 2026-10-20. This document describes goals and
acceptance criteria; it does not claim deployed voice support.

## Goal

People should be able to talk to a resident agent using the communication tools
they already use with teammates. The resident retains its identity, conversation
context, and task responsibilities across text and voice interactions.

The desired experiences are:

- Send a Slack audio message and have the resident understand it, answer a
  question, or accept a task.
- Invite the resident into a Slack huddle, where it listens for context and
  responds when addressed by name. It should distinguish general conversation
  from a request directed to it, and avoid interrupting unrelated discussion.
- Call a phone number associated with the resident and hold a two-way spoken
  conversation.
- Respect availability: a resident working on a task may decline or miss a call.
  Asynchronous messages remain available for later processing. Busy state should
  be visible rather than mistaken for a failed interaction.

## First milestone: Slack voice messages

Start with a recorded Slack clip and a text response in its thread. A user posts
an audio clip in a channel the resident can access, then mentions the resident in
that message or its thread. An explicit text mention selects the recipient for
this milestone; recognizing a spoken name is a later live-conversation feature.

The channel adapter locates the clip in the readable conversation, obtains an
accessible transcript, and presents it as message context with its author,
message timestamp, file reference, and transcript provenance. The resident uses
its existing task and reply flow to answer or act. Existing permission and
approval requirements for actions continue to apply to spoken requests.

Prefer Slack-provided clip transcripts for the smallest implementation. Verify
actual bot access and transcript shape with a real recording before relying on
that path. If a transcript is still processing, retry within a bounded window.
If Slack provides no usable transcript, report that clearly. Downloading audio
and invoking a transcription provider is part of the first milestone, with bounded file
size, duration, timeouts, and authenticated file access. Do not silently treat an
empty transcript as an empty request or claim to have heard inaccessible audio.

Use existing task scheduling when the resident is busy. A queued request must
retain its source and deduplication identity so retries do not produce duplicate
tasks or replies. Distinguish receipt, task acceptance, and task completion.
Text replies are sufficient for the first milestone; synthesized audio replies
can follow when useful.

### Acceptance

1. A person records a real Slack voice clip containing a question and mentions
   the intended resident in its thread.
2. Using that resident's Slack access, the integration retrieves the exact clip
   and usable transcript, preserving the sender and thread association.
3. The resident answers the question from the recording in the same thread.
4. A task request follows the normal task lifecycle; a busy resident can process
   it later without losing or duplicating it.
5. Unavailable transcripts, inaccessible files, and provider failures produce
   explicit outcomes. Repeated delivery does not duplicate work.

Fixture tests establish parsing and failure behavior. Completion also requires
a live human-recorded clip and an observed resident response; reading a file or
passing local tests alone does not establish the end-to-end experience.

## Provider-independent audio design

Keep communication transport separate from speech inference:

```text
Slack / other messaging service       Future call transport
             |                                |
 authenticated clip bytes              timestamped audio frames
             |                                |
    batch transcription adapter        streaming recognition adapter
             |                                |
             +----- attributed transcript ----+
                              |
                  resident conversation / task flow
                              |
                  text reply (speech output later)
```

### Small initial contract

The transport owns provider authentication, message/thread/sender identity, and
file retrieval. It supplies bounded audio bytes plus MIME type and an opaque
source reference. Do not pass Slack bearer tokens or private Slack URLs to an
inference provider. For managed Slack, authorized file retrieval belongs behind
the existing credential-holding service; verify its file scopes and add a narrow
file-read operation tied to a readable message, rather than a general URL proxy.

The transcription adapter owns model/API differences. The first contract is a
single cancellable batch operation, conceptually:

```typescript
interface Transcriber {
  transcribe(input: {
    audio: Uint8Array;
    mimeType: string;
    languageHint?: string;
    signal: AbortSignal;
  }): Promise<{
    text: string;
    language?: string;
    segments?: Array<{ startMs: number; endMs: number; text: string }>;
    backend: string;
    model: string;
  }>;
}
```

The orchestration layer associates results with tenant, resident, sender,
conversation, message, and attachment identity. It records whether text came
from the communication provider or independent recognition, and distinguishes
success, no speech, unsupported media, timeout, and unavailable backend. Native
provider transcript states are normalized at the transport boundary. Only a
usable completed transcript bypasses recognition; `none` triggers fallback,
while explicitly pending transcription may receive a short bounded wait.

Decode/resample audio when required by the selected backend, preserving duration
and channel information. Limit decoded duration as well as download size. Cache
results within the tenant by audio digest plus model/version/options, and
separately deduplicate task delivery by the original event identity. Transcript
caching must not let a repeated or revised event execute a task twice.

### Local and hosted backends

Start with one local HTTP transcription service and one implementation of the
interface. Keep its model loaded between requests. A self-hosted deployment can
run the service on the same machine or point it at a LAN GPU server; a resident
does not need its own model instance. Backend configuration selects endpoint,
model, optional credential reference, deadline, and concurrency limit. An
OpenAI-style transcription wire format can be convenient, but each adapter must
handle actual endpoint/field/response differences; compatibility is not assumed.

The initial candidate is `whisper.cpp` with Vulkan on an AMD workstation. It
supports CPU execution as well as Vulkan and NVIDIA CUDA builds, making it a
reasonable portable first experiment. Benchmark a small multilingual model
first; increase model size only when accuracy requires it. A Spark deployment is
another placement of the service, subject to ARM64/CUDA build and model tests.
Existing LLM inference on a machine does not establish speech-model support.

Hosted speech recognition can implement the same interface later. Configure
`local-only`, `hosted-only`, or an explicitly enabled ordered fallback policy.
Default self-hosted installations to local-only: a local outage must not silently
upload recordings to an external service. Backends never receive resident tools
or authority to execute the transcribed request.

Keep the first release to a clip reader, local batch adapter, and existing text
reply flow. Do not require a provider registry, GPU scheduler, new CRD, or live
call infrastructure to ship it.

### Fast operation and future calls

Measure download, decode, queue wait, inference, and resident response latency
separately. Use warm models and bounded concurrency. Compare transcript accuracy
and warm/cold latency on actual short recordings before claiming performance on
either GPU. A proposed initial target is a warm transcript within two seconds
for an approximately ten-second clip on the chosen local host; this is a target,
not a measured result. Measure end-to-end Slack reply latency separately.

Calls need a separate optional streaming contract: ordered audio frames with
sample rate, channels, sequence, and timestamps in; partial/final transcript
events out. Speaker labels are optional observations, not authenticated user
identities. Keep voice activity detection, turn completion, interruption,
spoken-name addressing, and text-to-speech separate from batch transcription.
Both paths converge on attributed transcript turns for the resident. Do not
simulate full-duplex conversation by repeatedly invoking a file transcription
endpoint and assume batch-capable providers support streaming.

### Local environment check (2026-10-20)

The workstation exposes an AMD GPU using the `amdgpu` driver. The reachable DGX
Spark inference endpoint lists a GLM language model. Neither observation proves
an installed speech recognition service, compatible speech-model acceleration,
spare GPU capacity, or transcription latency. No inference service was changed
as part of this design work.

## Later milestones

### Live Slack huddle participation

Investigate and prove a supported way to join a real huddle and receive live
audio. Reading a saved huddle transcript is useful context but does not establish
live participation. Slack's Calls API integrates externally hosted calls into
Slack; it does not itself supply a huddle media connection.

Once media access is proven, add streaming recognition, speaker attribution,
name-based addressing, conversational turn-taking, and spoken responses. Joining
should be visible to participants. Define who can invite the resident and how it
leaves. Preserve useful conversation context without turning every utterance
into a task. Confirm ambiguous requests before acting.

Acceptance requires a real huddle where a person addresses the resident by name,
asks a question, and hears a relevant answer while the call is still active.
Measure latency, interruptions, and reconnect behavior in that environment.
Provider choice and feasibility remain open; no huddle integration is selected.

### Telephone conversations

Assign a callable number to a resident contact and connect incoming calls to a
two-way voice session. Define ringing, accepted, busy, declined, missed, and ended
states. A busy policy may allow a short conversation, decline a call, or offer
voicemail; its precise behavior remains a product decision. Caller identity and
authorization need explicit handling before account-specific actions.

Acceptance requires a real inbound call, spoken back-and-forth, and verified busy
or missed-call behavior. Telephony provider selection follows the Slack clip
milestone and a decision about the shared live conversation runtime.

## Ownership and boundaries

Consistent with [OPR-003](requirements/OPR-003-agents.md) and the
[architecture](architecture.md), Agent Operator provisions the resident runtime,
workspace, and credential projections. Audio handling, provider adapters,
transcription, conversation policy, and task composition belong in Channels and
the resident profile/runtime. This proposal does not require new provider-specific
CRD fields or put audio processing into the Kubernetes controller.

Provider credentials stay within their configured credential boundary. Retrieve
messages and files only through the resident's granted access; a transcript does
not broaden its authority. Keep transcripts as untrusted conversation content,
with source attribution. Define audio retention before persisting raw recordings;
avoid copying private recordings or transcripts into repository documentation.

## Implementation sequence and open checks

1. Inspect a real clip already sent to the resident under its own Slack binding.
   Confirm workspace/bot identity, message visibility, file access, transcript
   availability, and any missing scopes or managed-proxy methods.
2. Extend the existing Slack context reader for ordinary clips. Existing saved
   huddle-transcript support can inform this implementation but does not prove
   ordinary clip access.
3. Verify the live clip-to-resident-to-threaded-reply acceptance flow.
4. Include a local transcription fallback for clips without usable native
   transcripts; then investigate live huddle media access and telephone calls.

Direct messages are a desired natural entry point, but their event subscriptions,
scopes, and managed-binding policy must be verified separately. The first explicit
mention-in-channel flow avoids making DM support a prerequisite.

## Initial live observation (2026-10-20)

A read-only check through an existing resident's managed Slack binding located a
human-recorded audio clip in a mentioned thread. Authentication identified the
resident bot, and the binding allowed the channel and thread reads. The file was
an approximately 11-second `audio/mp4` Slack clip marked visible, but its returned
`transcription.status` was `none`. The subsequent resident reply was a generic
greeting; it did not demonstrate comprehension of the recording.

Slack currently documents a user action to generate a clip transcript. The
observed `none` state is consistent with generation not having been requested,
but the response does not establish why it was absent. Users can try Generate
transcript on desktop or long-press the message on mobile and select that action;
then re-read the file to check whether accessible transcript text appears. The
agent integration must also work without that manual step. No documented public
API to request generation has been established by this investigation.

This establishes message and file-metadata visibility, not successful audio
retrieval, transcription, or understanding. The first milestone must account for
clips without a Slack transcript. Verify the managed provider boundary can expose
authorized file retrieval before adding a transcription fallback. Private message
contents, identifiers, and file URLs are deliberately omitted from this document.

## Exact-clip transcription experiment (2026-10-20)

The user supplied a local copy of the Slack clip through the shared filesystem.
Its size (143,893 bytes) and duration (11.075918 seconds) match the previously
observed Slack attachment. No independent Slack byte digest was available.
Identical decoded 16 kHz mono PCM and cached `ggml-small.en-tdrz.bin` model bytes
were verified by SHA-256 on both hosts. Both ran Whisper.cpp 1.8.7 with English,
four CPU threads, default decoding, and text/JSON output.

| Host | Verified backend | Three measured CLI runs |
| --- | --- | --- |
| Workstation, Radeon RX 9070 XT | Vulkan | 0.439 s, 0.421 s, 0.415 s |
| DGX Spark, NVIDIA GB10 | CUDA | 2.200 s, 1.000 s, 0.781 s |

These wall times include launching a fresh CLI process and loading its model.
They exclude audio conversion, network transfer, and compilation. Local runs
followed the skill's probe and initial transcription, so local filesystem and
shader caches were already exercised. Spark's first run followed a new CUDA
build, and its existing LLM workload remained running. This is a short-clip
feasibility experiment, not a controlled GPU ranking or persistent-server latency
benchmark. All six raw transcripts were identical; accuracy has not been scored
against a human reference. The cached English model is not a multilingual or
large-model quality evaluation.

Both hosts demonstrate sub-second transcription on repeated runs of this clip.
The workstation is a practical first local backend for this deployment. A
persistent service, authenticated Slack download, resident task integration, and
the actual Slack response remain to be implemented and verified. This experiment
did not change the existing inference service or deploy a transcription service.
Private audio, transcripts, hashes, and runtime logs remain outside the repository.

## Local implementation (2026-10-20)

The Channels `feat/audio-transcription` worktree implements a batch `Transcriber`
contract, OpenAI-compatible and Whisper.cpp HTTP protocols, explicit endpoint /
model / credential configuration, a packaged `outfitter-transcribe` CLI, and
Slack audio context fallback. It accepts mounted inference API-key files but
requires a separately selected transcription model. Native Slack transcripts
bypass audio upload. No automatic local-to-hosted failover is enabled.

The webapp `feat/resident-audio-download` worktree adds a managed audio-download
operation that verifies current binding, channel membership, exact message/file
association, provider file metadata, and bounded downloads. Slack bot credentials
stay inside the platform. Platform deployment and a resident runtime update plus
backend configuration are still required to activate this for Luce.

The packaged CLI was exercised against a local Whisper.cpp HTTP server using the
original m4a clip. Automated tests cover hosted request construction and provider
boundaries; live hosted transcription is not yet verified. The temporary local server was stopped after validation. These are
local changes, not a release or a verified live Slack reply.

### Managed inference correction

Managed residents should use the existing Outfitter relay at
`http://127.0.0.1:4141/v1`, with `TRANSCRIPTION_API=openai` and an explicit
`TRANSCRIPTION_MODEL=gpt-transcribe`. The agent needs no provider key. The sidecar
attaches its rotating projected ServiceAccount token; the gateway resolves the
organization's saved providers through the web app. Provider Secret sync also
exists, but reading a generic cluster key bypasses the resident's resolution.

The webapp draft now extends that relay/gateway with bounded multipart audio,
shared admission leases, and resolved OpenAI credentials. The first managed
version supports official OpenAI endpoints only and does not substitute chat
models or silently switch providers. Local Whisper.cpp remains an explicit
Channels backend. A live chat request through Luce's current relay returned
HTTP 200 with `gpt-6-luna` on 2026-10-20. That verifies the credential path, not
transcription. Deployment and the exact-clip Slack check remain pending.

### Rollout tracking

Channels audio support merged in [Channels PR #90](https://github.com/ai-outfitter/channels/pull/90),
commit `8755d0cb34a56ed4b0b8a51f15fcfe83d6390163`. Luce's effective profile
now pins that commit through a read-only operator-managed ConfigMap mount; a
catalog pin alone would have been shadowed by the older image-baked profile.
Her transcription environment points to the loopback relay without an API key.

Gateway and managed Slack download delivery is tracked in
[webapp issue #483](https://github.com/ai-outfitter/webapp/issues/483), carrying
forward the tested implementation from draft PR #482 through canonical review.
Until its deployment and exact-clip validation finish, live managed voice support
remains unverified.

## Provider references

- [Slack clip recording and transcript controls](https://slack.com/help/articles/4406235165587-Record-audio-and-video-clips-in-Slack).
- [whisper.cpp](https://github.com/ggml-org/whisper.cpp): supported execution
  backends and HTTP server example; validate the pinned build on target hardware.

- [Slack file object](https://docs.slack.dev/reference/objects/file-object/):
  audio/video transcription fields and separate huddle transcript metadata.
- [Slack Calls API](https://docs.slack.dev/apis/web-api/using-the-calls-api/):
  external-call integration and call metadata.

These references describe provider interfaces, not verified permissions or
capabilities of a particular resident installation.
