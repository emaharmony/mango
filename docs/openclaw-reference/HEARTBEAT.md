# Heartbeat

## Purpose
Maintain lightweight project awareness and momentum support without taking autonomous high-risk actions.

## Default Heartbeat Behavior
When checking workspace state:
- identify the highest-priority active task
- identify blockers or waiting decisions
- note if a design doc is missing (required before M1)
- note if a ticket is ready for a Junie prompt
- note if clarification is required before implementation
- note if a milestone has been reached and user needs to be tagged

## Constraints
Do not:
- autonomously merge code
- autonomously change architecture
- autonomously change workflows
- autonomously introduce major dependencies
- autonomously push big direction changes
- push to GitHub without user approval (milestone tagging is notification only)

## Preferred Heartbeat Output
Keep heartbeat-driven observations brief and practical:
- current focus
- blocker, if any
- next recommended move
- whether user input is needed

## Rule
If there is ambiguity that could cause wasted work, ask clarifying questions before recommending execution.

## Gemma Local Assist (Token Conservation)
When exploring large codebases or drafting complex Junie prompts, use local Gemma to pre-process before loading into my own context:
- File summaries: `cat <file> | ollama run gemma4 'Summarize the public API in 5 bullet points. Be concise.'`
- Directory triage: pipe `find` + file list through Gemma to identify relevant files before reading
- Junie prompt drafting: describe goal to Gemma first, refine output, then fire to Junie
- PR description drafting: `git diff --name-only | ollama run gemma4 'Draft a PR description for these changes: ...'`
- Memory compression: before writing session memory, Gemma condenses key facts

Gemma endpoint: `http://localhost:11434` | Model: `gemma4`

## Idle Rule
If Jirby has been idle for more than 20 minutes with no active tasks or pending user input, go to sleep immediately. Do not run heartbeat checks, background polls, or any ambient monitoring during idle periods. Wake only when a new message arrives.

## Workflow Summary (for heartbeat context)
See AGENTS.md for the full canonical workflow. Short version:
1. User proposes idea → Jirby builds design doc as proposals → saved to docs/ in project repo
2. Jirby feeds Junie work via CLI continuously until milestone reached
3. Milestones: M1 = working demo | M2-Mn = versioned releases (pushed as projectname-V(n)) | Mn+1 = production-ready
4. Tag user at each milestone or blocker only
