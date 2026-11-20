"""Record only synthetic marker evidence, with explicit request classification."""
import json,re
MARKERS=('AH_LOCAL_FIRST','AH_LOCAL_SECOND','AH_RELAY_B','AH_RESUME_B','AH_LOCAL_C','AH_LOCAL_FRESH','AH_LOCAL_NEW','AH_RESUME_C','AH_MCP_PROBE','AH_EXTERNAL_STATUS','AH_LIVE_STATUS')

def evidence(raw):
    body=json.loads(raw)
    messages=body.get('messages',body.get('input',[]))
    if isinstance(messages,str): messages=[{'role':'user','content':messages}]
    users=[x for x in messages if isinstance(x,dict) and x.get('role')=='user']
    latest=users[-1].get('content','') if users else ''
    if isinstance(latest,list):
        latest=''.join(x.get('text','') for x in latest if isinstance(x,dict))
    # A recovered conversation can prefix its next user message with Codex's
    # model-switch and collaboration reminders, in addition to environment
    # context. They are not the user's synthetic marker.
    latest=re.sub(r'^\s*(?:(?:<environment_context>.*?</environment_context>|<system-reminder>.*?</system-reminder>)\s*)+', '', latest, count=1, flags=re.S)
    current=[m for m in MARKERS if latest.strip()==m]
    serialized=json.dumps(body)
    markers=[m for m in MARKERS if m in serialized]
    # Match the pinned Codex title instruction, rather than any occurrence of
    # "title" in tools, system instructions or the conversation history.
    title=any('Generate a concise, single-line task title' in json.dumps(x) for x in users)
    kind='main' if current else 'auxiliary-title' if title else 'unclassified'
    return {'kind':kind,'current':current,'history':markers}
