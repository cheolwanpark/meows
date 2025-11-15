import Link from 'next/link'
import { NewsItem } from '@/components/news-item'

// Mock data - in production this would come from an API
const mockNews = [
  {
    id: 1,
    title: 'Scientists Discover Cats Can Understand Quantum Physics',
    url: 'https://example.com/quantum-cats',
    source: 'Tech Meows',
    points: 342,
    comments: 87,
    timeAgo: '2h ago',
    author: 'whiskerscience'
  },
  {
    id: 2,
    title: 'New Framework Purrs Faster Than React',
    url: 'https://example.com/fast-framework',
    source: 'Dev Digest',
    points: 256,
    comments: 124,
    timeAgo: '4h ago',
    author: 'codekitty'
  },
  {
    id: 3,
    title: 'AI Startup Raises $50M to Teach Machines to Nap Efficiently',
    url: 'https://example.com/ai-napping',
    source: 'Startup News',
    points: 189,
    comments: 45,
    timeAgo: '5h ago',
    author: 'sleepydev'
  },
  {
    id: 4,
    title: 'The Art of Clean Code: Lessons from Cat Grooming',
    url: 'https://example.com/clean-code',
    source: 'Programming Weekly',
    points: 423,
    comments: 156,
    timeAgo: '7h ago',
    author: 'cleanwhiskers'
  },
  {
    id: 5,
    title: 'Why Your Database Needs Nine Lives: A Guide to Redundancy',
    url: 'https://example.com/database-lives',
    source: 'Tech Meows',
    points: 301,
    comments: 92,
    timeAgo: '9h ago',
    author: 'dbkitty'
  },
]

export default function HomePage() {
  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <header className="border-b border-border bg-[#ff6b35] sticky top-0 z-10">
        <div className="max-w-5xl mx-auto px-4 py-3 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="text-3xl">🐱</div>
            <h1 className="text-xl font-bold text-white">Meows</h1>
            <span className="text-white/80 text-sm hidden sm:inline">
              News that matters, purr-fectly curated
            </span>
          </div>
          <Link 
            href="/config"
            className="flex items-center gap-2 text-white/90 hover:text-white transition-colors text-sm"
          >
            <span className="text-lg">⚙️</span>
            <span className="hidden sm:inline">Sources</span>
          </Link>
        </div>
      </header>

      {/* Main content */}
      <main className="max-w-5xl mx-auto px-4 py-6">
        <div className="space-y-3">
          {mockNews.map((item, index) => (
            <NewsItem key={item.id} item={item} index={index + 1} />
          ))}
        </div>

        {/* Footer */}
        <footer className="mt-12 py-6 text-center text-muted-foreground text-sm border-t border-border">
          <div className="flex items-center justify-center gap-2 mb-2">
            <span className="text-xl">😺</span>
            <span>Made with whiskers and code</span>
          </div>
          <div className="text-xs">
            Meows - Your friendly neighborhood news aggregator
          </div>
        </footer>
      </main>
    </div>
  )
}
