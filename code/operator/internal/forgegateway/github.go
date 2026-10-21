package forgegateway

import "strings"

const (
	providerGitHub        = "github"
	eventReviewComment    = "pull_request_review_comment"
	providerForgejo       = "forgejo"
	eventIssues           = "issues"
	eventPullRequest      = "pull_request"
	eventIssueComment     = "issue_comment"
	eventReview           = "pull_request_review"
	reasonAssigned        = "assigned"
	actionCreated         = "created"
	reasonReviewRequested = "review_requested"
	reasonMentioned       = "mentioned"
	workflowImplement     = "implement"
	workflowRespond       = "respond"
	workflowReview        = "review"
	workflowRevise        = "revise"
)

// These instructions are selected by verified event routing, not supplied by
// an issue body. They keep the resident's general implementation capability
// distinct from the workflow requested by this particular delivery.
func githubWorkflowInstruction(workflow string) string {
	switch workflow {
	case workflowImplement:
		return "Implement the assigned issue as a draft pull request. Read the issue and repository instructions first. Check for an existing implementation or linked pull request and continue that work rather than creating a duplicate. Request review from a different agent. Do not merge."
	case workflowRespond:
		return "Respond to the referenced mention, question, or thread reply. Read the exact thread first. The subject's standing assignment is context, not a new implementation request. Answer questions conversationally; implement only if the addressed request explicitly asks you to implement. If no response is called for, complete the task without posting."
	case workflowReview:
		return "Review the referenced pull request and its current head. Read existing reviews and avoid repeating a review of an unchanged head. Submit the review and complete this task. Do not start another implementation or merge."
	case workflowRevise:
		return "Address the submitted review on your existing pull request. Read the review and current head, fix or explain each finding, and continue the existing branch and pull request. Do not create a new issue implementation or merge."
	default:
		return ""
	}
}

// githubTargets routes the event's action, never the subject's standing
// assignment. A comment on an assigned issue is still a conversation event.
// GitHub includes the changed assignee explicitly, so this does not depend on
// reconstructing an assignment snapshot after a spool or volume migration.
func (g *Gateway) githubTargets(kind string, e event) map[string]forwarded {
	targets := map[string]forwarded{}
	base := githubSubject(e)
	add := func(username, reason, workflow string) {
		username = strings.ToLower(username)
		if _, ok := g.routes[username]; !ok || strings.EqualFold(username, base.Actor) {
			return
		}
		// An explicit assignment/review request wins over a mention in its body.
		if _, ok := targets[username]; ok {
			return
		}
		item := base
		item.Reason, item.Workflow = reason, workflow
		targets[username] = item
	}
	mention := func(body string) {
		for _, username := range mentions(body) {
			add(username, reasonMentioned, workflowRespond)
		}
	}
	switch kind {
	case eventIssues:
		routeGitHubIssue(e, add)
	case eventPullRequest:
		if e.PullRequest == nil {
			return targets
		}
		if e.Action == reasonReviewRequested && e.RequestedReviewer != nil {
			add(e.RequestedReviewer.name(), reasonReviewRequested, workflowReview)
		}
		if e.Action == actionOpened {
			mention(e.PullRequest.Body)
		}
	case eventIssueComment:
		if e.Comment == nil || e.Issue == nil || e.Action != actionCreated {
			return targets
		}
		base.URL = e.Comment.HTMLURL
		mention(e.Comment.Body)
		// Replies on an agent-authored thread are conversational too. Assignment
		// alone does not make an agent the addressee of every comment.
		if _, agentActor := g.routes[strings.ToLower(base.Actor)]; !agentActor {
			add(e.Issue.User.name(), "reply", workflowRespond)
		}
	case eventReview:
		if e.PullRequest == nil || e.Review == nil || e.Action != "submitted" {
			return targets
		}
		base.URL, base.HeadSHA = e.Review.HTMLURL, e.Review.CommitID
		if strings.EqualFold(e.Review.State, "changes_requested") {
			add(e.PullRequest.User.name(), "review_feedback", workflowRevise)
		} else if e.Review.Body != "" || strings.EqualFold(e.Review.State, "commented") {
			add(e.PullRequest.User.name(), "review_feedback", workflowRespond)
		}
		mention(e.Review.Body)
	case eventReviewComment:
		if e.PullRequest == nil || e.Comment == nil || e.Action != actionCreated || e.Comment.InReplyToID == 0 {
			return targets
		}
		base.URL = e.Comment.HTMLURL
		add(e.PullRequest.User.name(), "review_feedback", workflowRespond)
		mention(e.Comment.Body)
	}
	return targets
}

func githubSubject(e event) forwarded {
	base := forwarded{Repository: e.Repository.FullName, Actor: e.Sender.name()}
	if e.Issue != nil {
		base.Number, base.URL = e.Issue.Number, e.Issue.HTMLURL
	}
	if e.PullRequest != nil {
		base.Number, base.URL, base.HeadSHA = e.PullRequest.Number, e.PullRequest.HTMLURL, e.PullRequest.Head.SHA
	}
	return base
}

func routeGitHubIssue(e event, add func(string, string, string)) {
	if e.Issue == nil {
		return
	}
	if e.Action == reasonAssigned && e.Assignee != nil {
		add(e.Assignee.name(), reasonAssigned, workflowImplement)
	}
	if e.Action == actionOpened {
		// GitHub emits an assigned delivery separately, so do not also wake an
		// assignee through a mention in this same newly opened issue.
		for _, username := range mentions(e.Issue.Body) {
			assigned := false
			for _, user := range e.Issue.Assignees {
				if strings.EqualFold(user.name(), username) {
					assigned = true
					break
				}
			}
			if !assigned {
				add(username, reasonMentioned, workflowRespond)
			}
		}
	}
}
