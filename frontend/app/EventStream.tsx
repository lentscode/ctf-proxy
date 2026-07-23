import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getEvents, isUnauthorized, type ObserveEvent } from '../lib/api'
import { subscribeToEventStream, type StreamStatus } from '../lib/event-stream'

interface EventStreamProps {
  onUnauthorized: () => void
}

const maximumEvents = 100

function mergeEvents(...collections: ObserveEvent[][]): ObserveEvent[] {
  const unique = new Map<number, ObserveEvent>()
  for (const collection of collections) {
    for (const event of collection) {
      unique.set(event.id, event)
    }
  }
  return [...unique.values()].sort((left, right) => right.id - left.id).slice(0, maximumEvents)
}

export function EventStream({ onUnauthorized }: EventStreamProps) {
  const { data: history, error, isError, isLoading, refetch } = useQuery({ queryKey: ['events'], queryFn: getEvents })
  const [liveEvents, setLiveEvents] = useState<ObserveEvent[]>([])
  const [status, setStatus] = useState<StreamStatus>('connecting')
  const [hasNewEvents, setHasNewEvents] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const events = useMemo(() => mergeEvents(liveEvents, history ?? []), [history, liveEvents])

  useEffect(() => {
    if (isUnauthorized(error)) {
      onUnauthorized()
    }
  }, [error, onUnauthorized])

  useEffect(() => {
    const unsubscribe = subscribeToEventStream({
      onEvent(event) {
        const isReadingHistory = (scrollRef.current?.scrollTop ?? 0) > 12
        setLiveEvents((current) => mergeEvents([event], current))
        if (isReadingHistory) {
          setHasNewEvents(true)
        }
      },
      onConnected() {
        void refetch()
      },
      onStatus: setStatus,
      onUnauthorized,
    })
    return unsubscribe
  }, [onUnauthorized, refetch])

  function returnToNewest() {
    scrollRef.current?.scrollTo({ top: 0, behavior: 'smooth' })
    setHasNewEvents(false)
  }

  return (
    <section className="relative flex min-w-0 flex-col overflow-hidden border-l border-zinc-700 max-lg:border-l-0" aria-labelledby="events-heading">
      <header className="flex min-h-15 items-center border-b border-zinc-700 px-5">
        <h1 id="events-heading" className="text-base font-semibold tracking-tight text-zinc-100">Events</h1>
        <span className="ml-auto rounded-full bg-zinc-800 px-2 py-1 text-[11px] leading-none font-semibold text-zinc-300 capitalize">{status}</span>
      </header>
      <div className="min-h-0 flex-1 overflow-auto" ref={scrollRef} onScroll={() => {
        if ((scrollRef.current?.scrollTop ?? 0) <= 12) setHasNewEvents(false)
      }}>
        {isLoading && events.length === 0 && <EventSkeleton />}
        {isError && events.length === 0 && !isUnauthorized(error) && (
          <div className="grid min-h-45 place-content-center gap-3 p-6 text-center text-sm text-zinc-400">
            <p>Unable to load events.</p>
            <button type="button" className="justify-self-center bg-transparent p-0 text-sm text-zinc-200 underline underline-offset-3" onClick={() => void refetch()}>Retry</button>
          </div>
        )}
        {!isLoading && !isError && events.length === 0 && <div className="grid min-h-45 place-content-center p-6 text-center text-sm text-zinc-400">No events recorded.</div>}
        <ol className="m-0 list-none p-0">
          {events.map((event) => <EventRow key={event.id} event={event} />)}
        </ol>
      </div>
      {hasNewEvents && <button type="button" className="absolute right-4 bottom-4 h-8 cursor-pointer rounded-lg border border-zinc-200 bg-zinc-200 px-3 text-xs font-bold text-zinc-950 shadow-lg shadow-black/25" onClick={returnToNewest}>New events</button>}
    </section>
  )
}

function EventRow({ event }: { event: ObserveEvent }) {
  const metadata = [event.component, event.kind, event.proxy, event.filter, event.protocol, event.direction].filter(Boolean)
  const timestamp = new Date(event.time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  return (
    <li className="border-b border-zinc-700 px-5 py-3.5">
      <div className="grid grid-cols-[auto_auto_minmax(0,1fr)] items-start gap-2">
        <time dateTime={event.time} className="pt-0.5 font-mono text-[11px] leading-tight whitespace-nowrap text-zinc-400">{timestamp}</time>
        <span className="rounded-full bg-zinc-800 px-1.5 py-1 text-[11px] leading-none font-semibold text-zinc-200 uppercase">{event.level}</span>
        <p className="m-0 mt-0.5 break-words text-sm leading-snug text-zinc-100">{event.message}</p>
      </div>
      <p className="mt-1.5 ml-[93px] break-words font-mono text-[11px] leading-snug text-zinc-400">{metadata.join(' · ')}</p>
    </li>
  )
}

function EventSkeleton() {
  return <div className="grid gap-3 p-5" aria-label="Loading events">{Array.from({ length: 5 }, (_, index) => <span key={index} className="h-10 animate-pulse rounded bg-zinc-900" />)}</div>
}
