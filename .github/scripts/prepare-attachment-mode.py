#!/usr/bin/env python3
"""Temporary, branch-only source edit. Removed before the maintenance PR is merged."""
from pathlib import Path
import json


def replace_file(path, old, new, count=1):
    file = Path(path)
    text = file.read_text()
    assert text.count(old) == count, (path, old[:100], text.count(old))
    file.write_text(text.replace(old, new))


p = 'internal/model/teambridge.go'
replace_file(p, 'package model\n', 'package model\n\nimport "fmt"\n')
replace_file(p, '\tContext         TeamBridgeContext       `yaml:"context"`\n', '\tContext         TeamBridgeContext       `yaml:"context"`\n\t// Nil leaves the destination setting unmanaged, including old snapshots.\n\tAttachmentMode *string `yaml:"attachmentMode,omitempty"`\n')
replace_file(p, '\tContext         TeamBridgeContext       `json:"context"`\n', '\tContext         TeamBridgeContext       `json:"context"`\n\t// Nil means that the selected CLI/server did not expose the setting.\n\tAttachmentMode *string `json:"attachment_mode,omitempty"`\n')
replace_file(p, '\tContext          TeamBridgeContext\n', '\tContext          TeamBridgeContext\n\tAttachmentMode *string\n')
with Path(p).open('a') as f:
    f.write('''
// ValidateTeamBridgeAttachmentMode preserves omission rather than inventing a
// default. Unknown future modes must fail explicitly instead of losing state.
func ValidateTeamBridgeAttachmentMode(mode *string) error {
    if mode != nil && *mode != "none" && *mode != "links" {
        return fmt.Errorf("TeamBridge attachmentMode must be none or links; omit it to leave the setting unmanaged")
    }
    return nil
}
''')

p = 'internal/config/teambridge.go'
replace_file(p, '\tif d.Context.Mode == "" {\n', '\tif err := model.ValidateTeamBridgeAttachmentMode(d.AttachmentMode); err != nil {\n\t\treturn err\n\t}\n\tif d.Context.Mode == "" {\n')

p = 'internal/backend/teambridge.go'
replace_file(p, 'func teamBridgeSettingsBody(input model.TeamBridgeBotInput) (map[string]any, error) {\n', 'func teamBridgeSettingsBody(input model.TeamBridgeBotInput) (map[string]any, error) {\n\tif err := model.ValidateTeamBridgeAttachmentMode(input.AttachmentMode); err != nil {\n\t\treturn nil, err\n\t}\n')
replace_file(p, '\tbody := map[string]any{"context": input.Context, "access_mode": input.AccessMode, "denied_reply": input.DeniedReply}\n', '\tbody := map[string]any{"context": input.Context, "access_mode": input.AccessMode, "denied_reply": input.DeniedReply}\n\tif input.AttachmentMode != nil {\n\t\tbody["attachment_mode"] = *input.AttachmentMode\n\t}\n')
replace_file(p, '\tvar wireContext struct {\n', '''	if _, present := fields["attachment_mode"]; present && installation.AttachmentMode == nil {
        return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned null attachment_mode; update the fork CLI/server")
    }
    if err := model.ValidateTeamBridgeAttachmentMode(installation.AttachmentMode); err != nil {
        return model.TeamBridgeInstallation{}, err
    }
	var wireContext struct {
''')
replace_file(p, '\tif item.ID != id || item.Status != "active" {\n\t\treturn fmt.Errorf("TeamBridge update returned an unexpected or inactive installation")\n\t}\n', '''	if item.ID != id || item.Status != "active" {
        return fmt.Errorf("TeamBridge update returned an unexpected or inactive installation")
    }
    if input.AttachmentMode != nil && (item.AttachmentMode == nil || *item.AttachmentMode != *input.AttachmentMode) {
        return fmt.Errorf("TeamBridge update did not preserve attachmentMode; check fork CLI/server compatibility (the request may have applied other settings)")
    }
''')

p = 'internal/exporter/teambridge.go'
replace_file(p, '\t\t\tBotUsername: item.BotUsername, Status: item.Status, Context: item.Context,\n', '\t\t\tBotUsername: item.BotUsername, Status: item.Status, Context: item.Context,\n\t\t\tAttachmentMode: item.AttachmentMode,\n')
replace_file(p, '\t\tif item.AccessMode != "allowlist" && len(d.AllowedUsers) > 0 {\n', '''        if err := model.ValidateTeamBridgeAttachmentMode(item.AttachmentMode); err != nil {
            return fmt.Errorf("TeamBridge bot %q: %w", name, err)
        }
        if item.AttachmentMode == nil {
            s.warnings = append(s.warnings, fmt.Sprintf("TeamBridge bot %q: attachmentMode was not reported by the selected fork CLI/server and is left unmanaged; update both to export this setting", name))
        }
		if item.AccessMode != "allowlist" && len(d.AllowedUsers) > 0 {
''')

p = 'internal/reconcile/teambridge.go'
replace_file(p, '\tfor _, d := range p.TeamBridge {\n\t\tif _, exists := x.bots[d.Name]; exists {\n', '''	for _, d := range p.TeamBridge {
        if err := model.ValidateTeamBridgeAttachmentMode(d.AttachmentMode); err != nil {
            return fmt.Errorf("TeamBridge bot %q: %w", d.Name, err)
        }
		if _, exists := x.bots[d.Name]; exists {
''')
replace_file(p, 'func diffTeamBridge(d model.TeamBridgeBotSpec, a model.TeamBridgeInstallation, ops backend.TeamBridgeOperations) ([]string, []string, error) {\n\tfields := []string{}\n', '''func diffTeamBridge(d model.TeamBridgeBotSpec, a model.TeamBridgeInstallation, ops backend.TeamBridgeOperations) ([]string, []string, error) {
    if err := model.ValidateTeamBridgeAttachmentMode(d.AttachmentMode); err != nil {
        return nil, nil, err
    }
    fields := []string{}
    if d.AttachmentMode != nil {
        if a.AttachmentMode == nil {
            return nil, nil, fmt.Errorf("attachmentMode is managed but the selected fork CLI/server did not report attachment_mode; update both before applying (no changes applied)")
        }
        if *d.AttachmentMode != *a.AttachmentMode {
            fields = append(fields, "attachmentMode")
        }
    }
''')
replace_file(p, '\t\tServerURL: d.ServerURL, Context: d.Context, AccessMode: d.AccessMode,\n', '\t\tServerURL: d.ServerURL, Context: d.Context, AttachmentMode: d.AttachmentMode, AccessMode: d.AccessMode,\n')
replace_file(p, '\t\tin := teamBridgeInput(d, imageIDs)\n', '''        // Reactivation uses install. Preserve an observed destination value
        // when an old declaration leaves the new setting unmanaged.
        if o.install && o.actual.ID != "" && d.AttachmentMode == nil {
            d.AttachmentMode = o.actual.AttachmentMode
        }
		in := teamBridgeInput(d, imageIDs)
''')
replace_file(p, 'hasAnyField(o.change.Fields, "context", "accessMode", "allowedUsers", "deniedReply")', 'hasAnyField(o.change.Fields, "context", "attachmentMode", "accessMode", "allowedUsers", "deniedReply")')
replace_file(p, '\tbyName := map[string]string{}\n', '''    if d.AttachmentMode != nil && (a.AttachmentMode == nil || *a.AttachmentMode != *d.AttachmentMode) {
        return fmt.Errorf("server did not preserve requested attachmentMode; check fork CLI/server compatibility")
    }
	byName := map[string]string{}
''')

# Keep fixture semantics faithful to the fork: optional request fields preserve
# existing values. The flag exercises a server that silently ignores a field.
p = 'integration/teambridge_test.go'
replace_file(p, '\tmissingImage   bool\n', '\tmissingImage   bool\n\tignoreAttachmentMode bool\n')
replace_file(p, '\t\t\t"access_mode":       "allowlist",\n', '\t\t\t"access_mode":       "allowlist",\n\t\t\t"attachment_mode": "links",\n')
replace_file(p, '\t\trespond(f.installation)\n', '''        if mode, present := data["attachment_mode"]; present && !f.ignoreAttachmentMode {
            f.installation["attachment_mode"] = mode
        }
		respond(f.installation)
''', 2)

p = Path('integration/multica-teambridge.lock.json')
lock = json.loads(p.read_text())
assert lock['repository'] == 'Tr0sT/multica'
assert lock['revision'] == 'ba87df8702aeb026d15c05a997877d24eade46cf'
lock['revision'] = '6c5856c4a54ffbd296d96b812cb14225fd54e210'
p.write_text(json.dumps(lock, indent=2) + '\n')

for path in ('README.md', 'docs/managed-resources.md', 'docs/teambridge.md'):
    p = Path(path)
    text = p.read_text()
    assert '0.6.4' in text, path
    p.write_text(text.replace('0.6.4', '0.6.5').replace('October 20, 2026', 'October 21, 2026'))

p = 'docs/teambridge.md'
replace_file(p, 'status: active\ncontext:\n', 'status: active\nattachmentMode: links\ncontext:\n')
replace_file(p, '| `accessMode` | Yes |', '| `attachmentMode` | When reported by the fork CLI/server | `none` or `links`; omission leaves the destination unchanged |\n| `accessMode` | Yes |')
replace_file(p, '## Credentials and authentication\n', '''## Inbound attachments

`attachmentMode` controls how incoming Mattermost files reach the agent:

```yaml
attachmentMode: links
```

- `links` forwards incoming attachment metadata with on-demand links. It does not
  embed or pre-download every attachment into the agent context.
- `none` excludes incoming attachments. Explicit `none` is an actual setting,
  not the same as omitting the key.

The setting is independent of message/thread/channel context and refusal images.
Exports from the current fork preserve its exact value in `bot.yaml` and
`observed.json`. Both full and secretless snapshots manage this non-secret field.
Legacy YAML (or explicit YAML null) leaves it unmanaged: ordinary updates do not
send `attachment_mode`, and reactivation preserves an observed destination value.
New installations without it use the fork server's default.

Older CLIs/servers that do not report this field produce an export warning, and
no default value is invented. Applying an explicit value to an existing bot whose
CLI/server does not report the field fails during preflight, before any workspace
write. Unknown modes and null/invalid API values fail explicitly. Requested writes
are checked against the returned value; a server that ignores the field is not
reported as success. As with other apply errors, an API failure after a request
may leave partial changes. No compatibility patch or HTTP fallback is used.

Use the updated fork CLI **and** server. CI pins fork commit
`6c5856c4a54ffbd296d96b812cb14225fd54e210`, which already implements this setting;
this repository does not modify the fork.

## Credentials and authentication
''')
print('Scoped attachmentMode source changes prepared; run gofmt and both CI lanes.')
