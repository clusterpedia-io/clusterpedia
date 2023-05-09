# Clusterpedia Project Governance
## Membership
**Note**: This document is a work in progress

This doc outlines the various responsibilities of contributor roles in Clutserpedia.
|Role|Responsibilities|Defined by
|---|---|---|
|Member|Active contributor in the community| Clusterpedia GitHub org member|
|Reviewer|Review contributions from other members|[OWNERS](#submodel-and-owners-files) file reviewer entry|
|Approver|Contributions acceptance approval|[OWNERS](#submodel-and-owners-files) file approver entry|

## Members
Members are continuously active contributors in the community. They can have issues and PRs assigned to them.
Members are expected to remain active contributors to the community.

**Defined by**: Member of the Clusterpedia GitHub organization

### Requirements
* Enabled two-factor authentication on their GitHub account
* Have made multiple contributions to the project or community. Contribution may include, but is not limited to:
    * **Actively contributing to 1 or more subprojects. At least three PR must be merged.**
    * **Published several different articles or speeches about Clusterpedia.**
* [Open an membership issue](https://github.com/clusterpedia-io/clusterpedia/issues/new?assignees=Iceber&labels=kind%2Fmembership&template=membership-request.md&title=%5BMEMBERSHIP+REQUEST%5D+New+Member+of+Clusterpedia)
    * Make sure that the list of contributions included is representative of your work on the project.

### Responsibilities and privileges
* Responsive to issues and PRs assigned to them
* Members can do `/lgtm` on open PRs.
* They can be assigned to issues and PRs, and people can ask members for reviews with a `/cc @username.`

**Note**: Members who frequently contribute code are expected to proactively perform code reviews and work towards becoming a primary reviewer for the [submodel](#submodel-and-owners-files) that they are active in.

## Reviewers
Reviewers are able to review code for quality and correctness on the [submodel](#submodel-and-owners-files).
They are knowledgeable about both the codebase and software engineering principles.

**Defined by**: reviewers entry in an [OWNERS](#submodel-and-owners-files) file.

Reviewer status is scoped to a part of the codebase.

### Requirements
The following apply to the part of codebase for which one would be a reviewer in an [OWNERS](#submodel-and-owners-files) file (for repos using the bot).
* **member for at least 1 month**
* **Primary reviewer for at least 3 PRs to the codebase**
* **Reviewed or merged at least 10 substantial PRs to the codebase**
* Knowledgeable about the codebase
* Sponsored by a [submodel](#submodel-and-owners-files) approver
    * With no objections from other approvers
    * Done through PR to update the [OWNERS](#submodel-and-owners-files) file
* May either self-nominate, be nominated by an approver in this [submodel](#submodel-and-owners-files).

### Responsibilities and privileges
The following apply to the part of codebase for which one would be a reviewer in an [OWNERS](#submodel-and-owners-files) file (for repos using the bot).

* Code reviewer status may be a precondition to accepting large code contributions
* Responsible for project quality control
    * Focus on code quality and correctness, including testing and factoring
    * May also review for more holistic issues, but not a requirement
* Expected to be responsive to review requests
* Assigned PRs to review related to [submodel](#submodel-and-owners-files) of expertise
* Assigned test bugs related to [submodel](#submodel-and-owners-files) of expertise

## Approvers
Code approvers are able to both review and approve code contributions. While code review is focused on code quality and correctness, approval is focused on holistic acceptance of a contribution including: backwards / forwards compatibility, adhering to API and flag conventions, subtle performance and correctness issues, interactions with other parts of the system, etc.

**Defined by**: approvers entry in an [OWNERS](#submodel-and-owners-files) file

### Requirements
The following apply to the part of codebase for which one would be an approver in an [OWNERS](#submodel-and-owners-files) file (for repos using the bot).
* **Reviewer of the codebase for at least 2 months**
* **Primary reviewer for at least 10 substantial PRs to the codebase**
* **Reviewed or merged at least 30 PRs to the codebase**
* Nominated by a maintainer
    * With no objections from other maintainers
    * Done through PR to update the [OWNERS](#submodel-and-owners-files) file

**If you add a new submodule, then you can apply to be the Approver for that module, and the core participants of that module can be nominated by you as Reviewer**

### Responsibilities and privileges
The following apply to the part of codebase for which one would be an approver in an [OWNERS](#submodel-and-owners-files) file (for repos using the bot).

* Approver status may be a precondition to accepting large code contributions
* Demonstrate sound technical judgement
* Responsible for project quality control
    * Focus on holistic acceptance of contribution such as dependencies with other features, backwards / forwards compatibility, API and flag definitions, etc
* Expected to be responsive to review requests
* Mentor contributors and reviewers
* May approve code contributions for acceptance

## Submodel and OWNERS Files
For better maintenance of Clusterpedia, the repository is divided into the following modules:
* **APIServer**
    * cmd/apiserver
    * pkg/apiserver
    * pkg/kubeapiserver
    * pkg/storage/*
* **ClusterSynchro Manager**
    * cmd/clustersynchro
    * pkg/synchromanager
    * pkg/storage/*
* **Controller Manager**
    * cmd/controller-manager
    * pkg/controller
* **Internal Storage**
    * pkg/storage
    * pkg/storage/internalstorage
* **Memory Storage**
    * cmd/binding-apiserver
    * pkg/storage
    * pkg/storage/memorystorage
* **Deploy&Charts**
    * deploy
    * charts
* **E2E Tests**
    * tests

OWNERS files are used to designate responsibility over different models of the Clusterpedia codebase.
Today, we use them to assign the reviewer and approver roles that are used in our two-phase code review process.

OWNERS files are in YAML format and support the following keys:
* `approvers`: a list of GitHub usernames of the approver of the module
* `reviewers`: a list of GitHub usernames of the reviewer of the module

A typical OWNERS file like:
```yaml
approvers:
  - alice
  - bob     # this is a comment
reviewers:
  - alice
  - bob
  - carol
  - david   # this is another comment
```
