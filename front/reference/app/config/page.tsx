import Link from 'next/link'
import { SourceList } from '@/components/source-list'
import { AddSourceForm } from '@/components/add-source-form'

export default function ConfigPage() {
  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <header className="border-b border-border bg-[#ff6b35] sticky top-0 z-10">
        <div className="max-w-4xl mx-auto px-4 py-3 flex items-center justify-between">
          <Link href="/" className="flex items-center gap-3 text-white hover:text-white/90 transition-colors">
            <div className="text-3xl">🐱</div>
            <h1 className="text-xl font-bold">Meows</h1>
          </Link>
          <div className="text-white/80 text-sm">
            <span className="text-lg mr-2">⚙️</span>
            Configure Sources
          </div>
        </div>
      </header>

      {/* Main content */}
      <main className="max-w-4xl mx-auto px-4 py-8">
        {/* Add new source */}
        <section className="mb-10">
          <div className="flex items-center gap-2 mb-4">
            <span className="text-2xl">➕</span>
            <h2 className="text-xl font-semibold text-foreground">Add New Source</h2>
          </div>
          <div className="bg-muted/30 border border-border rounded-lg p-6">
            <AddSourceForm />
          </div>
        </section>

        {/* Current sources */}
        <section>
          <div className="flex items-center gap-2 mb-4">
            <span className="text-2xl">📰</span>
            <h2 className="text-xl font-semibold text-foreground">Your Sources</h2>
          </div>
          <SourceList />
        </section>

        {/* Help text */}
        <div className="mt-10 p-4 bg-muted/20 rounded-lg border border-border">
          <div className="flex items-start gap-3">
            <span className="text-2xl">💡</span>
            <div className="text-sm text-muted-foreground">
              <p className="mb-2">
                <strong className="text-foreground">Pro tip:</strong> Add RSS feeds or news APIs to customize your feed.
              </p>
              <p>
                Sources are fetched periodically to bring you the latest stories from across the web.
              </p>
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
