package bw

import "strings"

var guides = map[string]string{
	"planning": `Keep chains short and plans wide. Give the first wave several independent tasks.
Run bw shape before handing the plan to workers; start as many workers as the
most tasks ready at once. Avoid one large final task waiting on everything:
split runbooks, catalogues and integration work per area with a short final pass.

End each plan with review-and-fix by a different worker, at higher effort when
available. Tag the result when done. Serialize tasks editing the same files
with needs; keep each task to one area.

Use size for planning and statistics, effort and roles for who takes work,
and steps for visible progress. Point ref at a fixed brief with a checkable
done bar. Record gap decisions as addenda rather than editing the briefs.

Write a description of at most 200 characters saying what the plan is for;
update it when the catalog moves to a new plan. Set resources limits that fit
the workers' machines. Run bw check, then bw shape before starting the crew.`,
	"agents": `Claim before work, heartbeat while working, and finish or release before
stopping. Tick steps as you finish them. Declare --effort and --role when your
operator supplies them. Follow the project's review process before done.

Run bw idle to choose the next action. When it says wait, run bw wait --as NAME
--json and act on its output; never write your own polling loop. When it says
review, take the named review, follow the project's review process, then idle.

When stop names critical_resources: commit and push your work, tear down local
stacks, release your claim, add a note saying where the work stands, clean up,
and stop. Stop work before releasing unfinished tasks.

Clean up local stacks and scratch files as each task ends, not at the end of
the session. Heavy stacks left running fill disks and memory.`,
	"operating": `Start workers with one instruction naming the project's agent document and
saying whether to do one task or work continuously. Watch the board with bw tui
or bw web; answer coordinator questions there. Run bw todo for what's left.

Run bw doctor before starting a crew on a small machine. Size the crew using
bw shape. Upgrade every worker's bw before the catalog uses new fields;
bw version shows each build. The owner installs builds after merge.

Coordination is cooperative: Git credentials can bypass the CLI. Claims never
expire. Coordinate before release --force. Board text is pushed to origin;
never write secrets, prompts, customer data or command output.`,
}

func guide(o options) (outcome, error) {
	if len(o.args) == 0 {
		return outcome{result: "bw guide planning — shape a useful plan\nbw guide agents — work, wait and review\nbw guide operating — run a crew\nUse --markdown to copy a topic into repository docs."}, nil
	}
	topic := o.args[0]
	text, ok := guides[topic]
	if !ok {
		return outcome{}, fail(1, "Unknown guide topic %q; run bw guide", topic)
	}
	if o.markdown {
		text = "## Bullwhip guide: " + topic + "\n\n" + text
	}
	return outcome{result: strings.TrimSpace(text)}, nil
}
