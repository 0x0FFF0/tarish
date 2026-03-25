import { useEffect, useMemo, useState } from "react"
import { usePoll } from "@/hooks/use-poll"
import {
  api,
  type GuideDocument,
  type GuideDocumentInput,
  type GuideEditChallenge,
  type GuideEditSession,
  type GuideLink,
} from "@/lib/api"
import { cn, formatTimeAgo } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { BookCopy, Clipboard, FileCode2, FileText, PencilLine, RotateCcw, ShieldCheck } from "lucide-react"

type EditorState = {
  mode: "create" | "edit"
  documentId?: string
  draft: GuideDocumentInput
}

const emptyDraft: GuideDocumentInput = {
  title: "",
  summary: "",
  category: "general",
  content_type: "text",
  content: "",
  links: [],
}

const savePhrase = "SAVE"
const rollbackPhrase = "ROLLBACK"

export default function Guides() {
  const { data: docs, loading, error, refresh } = usePoll<GuideDocument[]>(() => api.getGuideDocuments(), 30000)
  const [selectedId, setSelectedId] = useState("")
  const [challenge, setChallenge] = useState<GuideEditChallenge | null>(null)
  const [challengeAnswer, setChallengeAnswer] = useState("")
  const [session, setSession] = useState<GuideEditSession | null>(null)
  const [editor, setEditor] = useState<EditorState | null>(null)
  const [saveArmed, setSaveArmed] = useState(false)
  const [saveConfirmValue, setSaveConfirmValue] = useState("")
  const [rollbackArmed, setRollbackArmed] = useState(false)
  const [rollbackConfirmValue, setRollbackConfirmValue] = useState("")
  const [copiedKey, setCopiedKey] = useState("")
  const [statusMessage, setStatusMessage] = useState("")
  const [busyAction, setBusyAction] = useState("")

  const sessionActive = Boolean(session && new Date(session.expires_at).getTime() > Date.now())
  const selectedDoc = useMemo(
    () => (docs ?? []).find(doc => doc.id === selectedId) ?? null,
    [docs, selectedId]
  )
  const deploymentDocs = useMemo(
    () => (docs ?? []).filter(doc => doc.content_type === "code"),
    [docs]
  )

  useEffect(() => {
    if (!docs || docs.length === 0) return
    if (!selectedId || !docs.some(doc => doc.id === selectedId)) {
      setSelectedId(docs[0].id)
    }
  }, [docs, selectedId])

  useEffect(() => {
    if (session && !sessionActive) {
      setSession(null)
      setEditor(null)
      setChallenge(null)
      setChallengeAnswer("")
      setSaveArmed(false)
      setRollbackArmed(false)
      setStatusMessage("Edit access expired. Run verification again to continue editing.")
    }
  }, [session, sessionActive])

  const startVerification = async () => {
    setBusyAction("challenge")
    setStatusMessage("")
    try {
      const nextChallenge = await api.startGuideEditChallenge()
      setChallenge(nextChallenge)
      setChallengeAnswer("")
    } catch (err) {
      setStatusMessage(errorMessage(err, "Unable to start verification."))
    } finally {
      setBusyAction("")
    }
  }

  const completeVerification = async () => {
    if (!challenge) return

    setBusyAction("verify")
    setStatusMessage("")
    try {
      const nextSession = await api.createGuideEditSession(challenge.challenge_id, challengeAnswer)
      setSession(nextSession)
      setChallenge(null)
      setChallengeAnswer("")
      setStatusMessage(`Editing unlocked until ${formatDateTime(nextSession.expires_at)}.`)
    } catch (err) {
      setStatusMessage(errorMessage(err, "Verification failed."))
    } finally {
      setBusyAction("")
    }
  }

  const openEditor = (mode: "create" | "edit") => {
    if (!sessionActive) {
      setStatusMessage("Complete verification before editing documents.")
      return
    }

    setStatusMessage("")
    setSaveArmed(false)
    setSaveConfirmValue("")

    if (mode === "create") {
      setEditor({
        mode,
        draft: { ...emptyDraft },
      })
      return
    }

    if (!selectedDoc) {
      setStatusMessage("Select a document before editing.")
      return
    }

    setEditor({
      mode,
      documentId: selectedDoc.id,
      draft: toDraft(selectedDoc),
    })
  }

  const closeEditor = () => {
    setEditor(null)
    setSaveArmed(false)
    setSaveConfirmValue("")
  }

  const updateDraft = <K extends keyof GuideDocumentInput>(key: K, value: GuideDocumentInput[K]) => {
    setEditor(current => {
      if (!current) return current
      return { ...current, draft: { ...current.draft, [key]: value } }
    })
  }

  const updateDraftLink = (index: number, key: keyof GuideLink, value: string) => {
    setEditor(current => {
      if (!current) return current
      const links = current.draft.links.map((link, i) => (i === index ? { ...link, [key]: value } : link))
      return { ...current, draft: { ...current.draft, links } }
    })
  }

  const addDraftLink = () => {
    setEditor(current => {
      if (!current) return current
      return {
        ...current,
        draft: {
          ...current.draft,
          links: [...current.draft.links, { label: "", url: "" }],
        },
      }
    })
  }

  const removeDraftLink = (index: number) => {
    setEditor(current => {
      if (!current) return current
      return {
        ...current,
        draft: {
          ...current.draft,
          links: current.draft.links.filter((_, i) => i !== index),
        },
      }
    })
  }

  const saveDocument = async () => {
    if (!editor || !sessionActive || !session) return
    if (saveConfirmValue.trim().toUpperCase() !== savePhrase) {
      setStatusMessage(`Type ${savePhrase} to confirm the document change.`)
      return
    }

    setBusyAction("save")
    setStatusMessage("")
    try {
      const sanitizedDraft = sanitizeDraft(editor.draft)
      const saved =
        editor.mode === "create"
          ? await api.createGuideDocument(sanitizedDraft, session.token)
          : await api.updateGuideDocument(editor.documentId!, sanitizedDraft, session.token)

      await refresh()
      setSelectedId(saved.id)
      closeEditor()
      setStatusMessage(`${saved.title} saved.`)
    } catch (err) {
      handleEditorError(err)
    } finally {
      setBusyAction("")
    }
  }

  const rollbackDocument = async () => {
    if (!selectedDoc || !sessionActive || !session) return
    if (rollbackConfirmValue.trim().toUpperCase() !== rollbackPhrase) {
      setStatusMessage(`Type ${rollbackPhrase} to restore the previous version.`)
      return
    }

    setBusyAction("rollback")
    setStatusMessage("")
    try {
      const rolledBack = await api.rollbackGuideDocument(selectedDoc.id, session.token)
      await refresh()
      setSelectedId(rolledBack.id)
      setRollbackArmed(false)
      setRollbackConfirmValue("")
      closeEditor()
      setStatusMessage(`${rolledBack.title} rolled back to the previous revision.`)
    } catch (err) {
      handleEditorError(err)
    } finally {
      setBusyAction("")
    }
  }

  const copyText = async (key: string, value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopiedKey(key)
      setStatusMessage("Copied to clipboard.")
      window.setTimeout(() => {
        setCopiedKey(current => (current === key ? "" : current))
      }, 1500)
    } catch {
      setStatusMessage("Clipboard access failed in this browser session.")
    }
  }

  const handleEditorError = (err: unknown) => {
    const message = errorMessage(err, "Guide update failed.")
    if (message.includes("guide edit token")) {
      setSession(null)
      setEditor(null)
      setChallenge(null)
    }
    setStatusMessage(message)
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-2 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Tarish Miner Guide</h1>
          <p className="text-muted-foreground">
            Read-only install docs by default, with verified editing, rollback, and copyable deployment blocks.
          </p>
        </div>
        {statusMessage && (
          <div className="rounded-md border border-border bg-card px-3 py-2 text-sm text-muted-foreground">
            {statusMessage}
          </div>
        )}
      </div>

      <div className="grid gap-4 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <CardHeader>
            <div className="flex items-center justify-between gap-4">
              <div>
                <CardTitle className="text-base">Quick Deployment Blocks</CardTitle>
                <CardDescription>Copy the current script or terminal snippets without opening the editor.</CardDescription>
              </div>
              <Badge variant="secondary">{deploymentDocs.length} code docs</Badge>
            </div>
          </CardHeader>
          <CardContent className="grid gap-4 md:grid-cols-2">
            {deploymentDocs.map(doc => (
              <div
                key={doc.id}
                className={cn(
                  "rounded-lg border border-border bg-background p-4 text-left transition-colors hover:border-primary/50 hover:bg-secondary/30",
                  selectedId === doc.id && "border-primary/70 bg-secondary/40"
                )}
              >
                <div className="mb-3 flex items-start justify-between gap-3">
                  <div>
                    <div className="flex items-center gap-2">
                      <FileCode2 className="h-4 w-4 text-primary" />
                      <p className="font-medium">{doc.title}</p>
                    </div>
                    <p className="mt-1 text-sm text-muted-foreground">{doc.summary}</p>
                  </div>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={event => {
                      event.stopPropagation()
                      void copyText(`block-${doc.id}`, doc.content)
                    }}
                  >
                    {copiedKey === `block-${doc.id}` ? "Copied" : "Copy"}
                  </Button>
                </div>
                <pre
                  className="max-h-44 cursor-pointer overflow-auto rounded-md border border-border/70 bg-black/20 p-3 text-xs leading-6 text-foreground/90"
                  onClick={() => setSelectedId(doc.id)}
                >
                  <code>{doc.content}</code>
                </pre>
              </div>
            ))}
            {deploymentDocs.length === 0 && (
              <div className="rounded-lg border border-dashed border-border p-6 text-sm text-muted-foreground">
                No deployment blocks yet. Create or convert a guide document to a code block after verification.
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <ShieldCheck className="h-4 w-4 text-primary" />
              <CardTitle className="text-base">Editing Access</CardTitle>
            </div>
            <CardDescription>
              Verification is required before the write interface appears. Sessions expire automatically.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {sessionActive && session ? (
              <div className="space-y-3">
                <div className="rounded-lg border border-primary/30 bg-primary/10 p-3 text-sm">
                  Editing unlocked until {formatDateTime(session.expires_at)}.
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" onClick={() => openEditor("edit")} disabled={!selectedDoc}>
                    <PencilLine className="mr-2 h-4 w-4" />
                    Edit Selected
                  </Button>
                  <Button variant="secondary" size="sm" onClick={() => openEditor("create")}>
                    <FileText className="mr-2 h-4 w-4" />
                    New Document
                  </Button>
                </div>
              </div>
            ) : (
              <div className="space-y-3">
                {!challenge ? (
                  <Button onClick={() => void startVerification()} disabled={busyAction === "challenge"}>
                    {busyAction === "challenge" ? "Preparing..." : "Start Verification"}
                  </Button>
                ) : (
                  <>
                    <div className="rounded-lg border border-border bg-background p-3 text-sm text-muted-foreground">
                      {challenge.prompt}
                    </div>
                    <input
                      type="text"
                      placeholder="Enter verification code"
                      className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      value={challengeAnswer}
                      onChange={event => setChallengeAnswer(event.target.value)}
                    />
                    <div className="flex gap-2">
                      <Button onClick={() => void completeVerification()} disabled={busyAction === "verify"}>
                        {busyAction === "verify" ? "Verifying..." : "Unlock Editing"}
                      </Button>
                      <Button variant="ghost" onClick={() => setChallenge(null)}>
                        Cancel
                      </Button>
                    </div>
                  </>
                )}
              </div>
            )}

            {selectedDoc?.can_rollback && sessionActive && (
              <div className="space-y-3 rounded-lg border border-border p-3">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <RotateCcw className="h-4 w-4 text-chart-5" />
                  Rollback previous revision
                </div>
                {!rollbackArmed ? (
                  <Button variant="outline" size="sm" onClick={() => setRollbackArmed(true)}>
                    Prepare Rollback
                  </Button>
                ) : (
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">
                      Type {rollbackPhrase} to restore the most recent saved revision.
                    </p>
                    <input
                      type="text"
                      className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      value={rollbackConfirmValue}
                      onChange={event => setRollbackConfirmValue(event.target.value)}
                    />
                    <div className="flex gap-2">
                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={() => void rollbackDocument()}
                        disabled={busyAction === "rollback"}
                      >
                        {busyAction === "rollback" ? "Rolling Back..." : "Confirm Rollback"}
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          setRollbackArmed(false)
                          setRollbackConfirmValue("")
                        }}
                      >
                        Cancel
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 xl:grid-cols-[320px_minmax(0,1fr)]">
        <Card className="h-fit">
          <CardHeader>
            <CardTitle className="text-base">Document Library</CardTitle>
            <CardDescription>Switch between install docs, script blocks, and terminal references.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            {(docs ?? []).map(doc => (
              <button
                key={doc.id}
                type="button"
                onClick={() => setSelectedId(doc.id)}
                className={cn(
                  "w-full rounded-lg border border-transparent p-3 text-left transition-colors hover:bg-secondary/40",
                  selectedId === doc.id && "border-primary/50 bg-secondary/40"
                )}
              >
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="flex items-center gap-2">
                      {doc.content_type === "code" ? (
                        <FileCode2 className="h-4 w-4 text-primary" />
                      ) : (
                        <FileText className="h-4 w-4 text-chart-2" />
                      )}
                      <p className="text-sm font-medium">{doc.title}</p>
                    </div>
                    <p className="mt-1 text-xs leading-5 text-muted-foreground">{doc.summary}</p>
                  </div>
                  <Badge variant={badgeVariantForCategory(doc.category)}>{labelForCategory(doc.category)}</Badge>
                </div>
              </button>
            ))}
            {!loading && (docs ?? []).length === 0 && (
              <p className="text-sm text-muted-foreground">No guide documents available.</p>
            )}
            {error && <p className="text-sm text-destructive">{error.message}</p>}
          </CardContent>
        </Card>

        <div className="space-y-4">
          {selectedDoc ? (
            <Card>
              <CardHeader className="space-y-3">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                  <div className="space-y-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <CardTitle>{selectedDoc.title}</CardTitle>
                      <Badge variant={badgeVariantForCategory(selectedDoc.category)}>
                        {labelForCategory(selectedDoc.category)}
                      </Badge>
                      {selectedDoc.can_rollback && <Badge variant="outline">{selectedDoc.revision_count} revisions</Badge>}
                    </div>
                    <CardDescription>{selectedDoc.summary}</CardDescription>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-xs text-muted-foreground">
                      Updated {formatTimeAgo(selectedDoc.updated_at)}
                    </span>
                    {selectedDoc.content_type === "code" && (
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => void copyText(`viewer-${selectedDoc.id}`, selectedDoc.content)}
                      >
                        <Clipboard className="mr-2 h-4 w-4" />
                        {copiedKey === `viewer-${selectedDoc.id}` ? "Copied" : "Copy Block"}
                      </Button>
                    )}
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-6">
                {selectedDoc.content_type === "code" ? (
                  <div className="space-y-4">
                    <pre className="overflow-x-auto rounded-lg border border-border bg-black/20 p-4 text-sm leading-7">
                      <code>{selectedDoc.content}</code>
                    </pre>

                    {extractCommands(selectedDoc.content).length > 0 && (
                      <div className="space-y-2">
                        <div className="flex items-center gap-2 text-sm font-medium">
                          <BookCopy className="h-4 w-4 text-primary" />
                          One-click command lines
                        </div>
                        <div className="space-y-2">
                          {extractCommands(selectedDoc.content).map((command, index) => (
                            <div
                              key={`${selectedDoc.id}-${index}`}
                              className="flex flex-col gap-2 rounded-lg border border-border bg-background p-3 lg:flex-row lg:items-center lg:justify-between"
                            >
                              <code className="text-xs leading-6 text-foreground/90">{command}</code>
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => void copyText(`command-${selectedDoc.id}-${index}`, command)}
                              >
                                {copiedKey === `command-${selectedDoc.id}-${index}` ? "Copied" : "Copy Line"}
                              </Button>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="rounded-lg border border-border bg-background p-4 text-sm leading-7 whitespace-pre-line">
                    {selectedDoc.content}
                  </div>
                )}

                {selectedDoc.links.length > 0 && (
                  <div className="space-y-3">
                    <div className="text-sm font-medium">Related links</div>
                    <div className="grid gap-2 md:grid-cols-2">
                      {selectedDoc.links.map(link => (
                        <a
                          key={`${selectedDoc.id}-${link.label}-${link.url}`}
                          href={link.url}
                          target={link.url.startsWith("/") ? undefined : "_blank"}
                          rel={link.url.startsWith("/") ? undefined : "noreferrer"}
                          className="rounded-lg border border-border bg-background px-3 py-2 text-sm transition-colors hover:border-primary/40 hover:text-primary"
                        >
                          <div className="font-medium">{link.label}</div>
                          <div className="mt-1 text-xs text-muted-foreground">{link.url}</div>
                        </a>
                      ))}
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardContent className="py-12 text-center text-muted-foreground">
                {loading ? "Loading guide documents..." : "Select a guide document to view it."}
              </CardContent>
            </Card>
          )}

          {editor && sessionActive && (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">
                  {editor.mode === "create" ? "Create Guide Document" : "Edit Guide Document"}
                </CardTitle>
                <CardDescription>
                  Changes remain blocked until you confirm the save. Rollback is available from the access card after a revision exists.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="grid gap-4 lg:grid-cols-2">
                  <label className="space-y-2 text-sm">
                    <span className="font-medium">Title</span>
                    <input
                      type="text"
                      className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      value={editor.draft.title}
                      onChange={event => updateDraft("title", event.target.value)}
                    />
                  </label>
                  <label className="space-y-2 text-sm">
                    <span className="font-medium">Category</span>
                    <select
                      className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      value={editor.draft.category}
                      onChange={event => updateDraft("category", event.target.value)}
                    >
                      <option value="general">General Guide</option>
                      <option value="script">Remote Script</option>
                      <option value="terminal">Terminal Block</option>
                    </select>
                  </label>
                </div>

                <label className="space-y-2 text-sm">
                  <span className="font-medium">Summary</span>
                  <input
                    type="text"
                    className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    value={editor.draft.summary}
                    onChange={event => updateDraft("summary", event.target.value)}
                  />
                </label>

                <label className="space-y-2 text-sm">
                  <span className="font-medium">Document Type</span>
                  <select
                    className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    value={editor.draft.content_type}
                    onChange={event => updateDraft("content_type", event.target.value)}
                  >
                    <option value="text">Read-only text</option>
                    <option value="code">Code block</option>
                  </select>
                </label>

                <label className="space-y-2 text-sm">
                  <span className="font-medium">Content</span>
                  <textarea
                    className={cn(
                      "min-h-72 w-full rounded-md border border-input bg-background px-3 py-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                      editor.draft.content_type === "code" && "font-mono leading-6"
                    )}
                    value={editor.draft.content}
                    onChange={event => updateDraft("content", event.target.value)}
                  />
                </label>

                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <div className="text-sm font-medium">Related links</div>
                      <div className="text-xs text-muted-foreground">Use internal paths or full `http(s)` URLs.</div>
                    </div>
                    <Button variant="outline" size="sm" onClick={addDraftLink}>
                      Add Link
                    </Button>
                  </div>

                  <div className="space-y-2">
                    {editor.draft.links.map((link, index) => (
                      <div key={`link-${index}`} className="grid gap-2 rounded-lg border border-border p-3 lg:grid-cols-[1fr_1.2fr_auto]">
                        <input
                          type="text"
                          placeholder="Label"
                          className="h-10 rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                          value={link.label}
                          onChange={event => updateDraftLink(index, "label", event.target.value)}
                        />
                        <input
                          type="text"
                          placeholder="/miners or https://..."
                          className="h-10 rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                          value={link.url}
                          onChange={event => updateDraftLink(index, "url", event.target.value)}
                        />
                        <Button variant="ghost" size="sm" onClick={() => removeDraftLink(index)}>
                          Remove
                        </Button>
                      </div>
                    ))}
                    {editor.draft.links.length === 0 && (
                      <div className="rounded-lg border border-dashed border-border p-3 text-sm text-muted-foreground">
                        No links configured for this document.
                      </div>
                    )}
                  </div>
                </div>

                {!saveArmed ? (
                  <div className="flex flex-wrap gap-2">
                    <Button onClick={() => setSaveArmed(true)}>Review Save</Button>
                    <Button variant="ghost" onClick={closeEditor}>
                      Cancel
                    </Button>
                  </div>
                ) : (
                  <div className="space-y-3 rounded-lg border border-border p-4">
                    <div className="text-sm font-medium">Save confirmation</div>
                    <p className="text-sm text-muted-foreground">
                      Type {savePhrase} to confirm this change. The previous document state will be stored for rollback.
                    </p>
                    <input
                      type="text"
                      className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      value={saveConfirmValue}
                      onChange={event => setSaveConfirmValue(event.target.value)}
                    />
                    <div className="flex flex-wrap gap-2">
                      <Button onClick={() => void saveDocument()} disabled={busyAction === "save"}>
                        {busyAction === "save" ? "Saving..." : "Confirm Save"}
                      </Button>
                      <Button
                        variant="ghost"
                        onClick={() => {
                          setSaveArmed(false)
                          setSaveConfirmValue("")
                        }}
                      >
                        Back
                      </Button>
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>
          )}
        </div>
      </div>
    </div>
  )
}

function toDraft(doc: GuideDocument): GuideDocumentInput {
  return {
    title: doc.title,
    summary: doc.summary,
    category: doc.category,
    content_type: doc.content_type,
    content: doc.content,
    links: doc.links.map(link => ({ ...link })),
  }
}

function sanitizeDraft(draft: GuideDocumentInput): GuideDocumentInput {
  return {
    title: draft.title.trim(),
    summary: draft.summary.trim(),
    category: draft.category.trim(),
    content_type: draft.content_type.trim(),
    content: draft.content,
    links: draft.links
      .map(link => ({ label: link.label.trim(), url: link.url.trim() }))
      .filter(link => link.label || link.url),
  }
}

function extractCommands(content: string): string[] {
  return content
    .split("\n")
    .map(line => line.trim())
    .filter(line => line.length > 0 && !line.startsWith("#"))
}

function labelForCategory(category: string): string {
  switch (category) {
    case "script":
      return "Remote Script"
    case "terminal":
      return "Terminal"
    default:
      return "Guide"
  }
}

function badgeVariantForCategory(category: string): "secondary" | "warning" | "outline" {
  switch (category) {
    case "script":
      return "secondary"
    case "terminal":
      return "warning"
    default:
      return "outline"
  }
}

function formatDateTime(value: string): string {
  return new Date(value).toLocaleString()
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}
