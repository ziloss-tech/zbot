export function KnowledgeBasePage() {
  return (
    <div className="flex h-full flex-col items-center justify-center p-6 text-center">
      <span className="text-4xl opacity-30">📚</span>
      <h3 className="mt-4 font-display text-lg font-bold text-gray-300">Knowledge Base</h3>
      <p className="mt-2 font-mono text-xs text-gray-500 max-w-sm">
        Coming in Sprint 19 — upload docs, SOPs, pricing sheets, and ZBOT will always have context on your business without you having to repeat yourself.
      </p>
      <div className="mt-6 space-y-2 text-left w-full max-w-sm">
        {[
          'Lead Certain playbook & pricing rules',
          'Ziloss CRM PRD & competitive positioning',
          'GHL location IDs & workflow docs',
          'Esler account details & contacts',
        ].map((item) => (
          <div key={item} className="flex items-center gap-3 rounded-md border border-surface-600 bg-surface-800 px-4 py-2.5 opacity-40">
            <span className="font-mono text-xs text-gray-500">○</span>
            <span className="font-mono text-xs text-gray-400">{item}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
