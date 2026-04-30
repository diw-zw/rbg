import { useMemo, useRef, useEffect, useState } from 'react'
import type { TrialResult, ParamSet } from '@/types'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { formatNumber, computeScoreRatio, scoreColorToHsl } from '@/lib/utils'

interface ParallelCoordinatesProps {
  trials: TrialResult[]
  optimize: string
  height?: number
}

interface CoordData {
  label: string
  min: number
  max: number
  values: number[]
  isMetric: boolean
}

export function ParallelCoordinates({ trials, optimize, height = 420 }: ParallelCoordinatesProps) {
  const svgRef = useRef<SVGSVGElement>(null)
  const [hoveredTrial, setHoveredTrial] = useState<number | null>(null)
  const [dimensions, setDimensions] = useState({ w: 800, h: height })

  useEffect(() => {
    const onResize = () => {
      if (svgRef.current) {
        const rect = svgRef.current.getBoundingClientRect()
        setDimensions({ w: rect.width || 800, h: height })
      }
    }
    onResize()
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [height])

  const { axes, trialLines, allTrials } = useMemo(() => {
    if (trials.length === 0) return { axes: [], trialLines: [], allTrials: [] }

    // Collect parameter names from all trials
    const paramNames = new Set<string>()
    trials.forEach(t => {
      const p = t.params.default || {}
      Object.keys(p).forEach(k => paramNames.add(k))
    })
    const sortedParams = Array.from(paramNames).sort()

    // Build axes: parameters + the optimize metric (score) at the end
    const axes: CoordData[] = sortedParams.map(name => ({
      label: name,
      min: 0,
      max: 0,
      values: [],
      isMetric: false,
    }))

    // Add score axis at the end
    axes.push({
      label: optimize,
      min: 0,
      max: 0,
      values: [],
      isMetric: true,
    })

    // Collect values
    trials.forEach(t => {
      const p = t.params.default || {}
      sortedParams.forEach((name, i) => {
        const v = p[name]
        if (typeof v === 'number') {
          axes[i].values.push(v)
        }
      })
      axes[axes.length - 1].values.push(t.slaPass ? t.score : 0)
    })

    // Compute min/max for each axis with padding
    axes.forEach(axis => {
      if (axis.values.length === 0) { axis.min = 0; axis.max = 1; return }
      let mn = Infinity, mx = -Infinity
      axis.values.forEach(v => { if (v < mn) mn = v; if (v > mx) mx = v })
      const pad = (mx - mn) * 0.05 || 1
      axis.min = mn - pad
      axis.max = mx + pad
    })

    // Normalize values and build trial lines
    const scores = trials.map(t => t.score)
    const trialLines = trials.map((t, trialIdx) => {
      const p = t.params.default || {}
      const points: { x: number; y: number }[] = []

      sortedParams.forEach((name, i) => {
        const v = p[name]
        if (typeof v === 'number') {
          const ratio = axes[i].max === axes[i].min ? 0.5 : (v - axes[i].min) / (axes[i].max - axes[i].min)
          points.push({ x: i, y: 1 - ratio })
        }
      })

      // Score point
      const scoreV = t.slaPass ? t.score : 0
      const scoreRatio = axes[axes.length - 1].max === axes[axes.length - 1].min
        ? 0.5
        : (scoreV - axes[axes.length - 1].min) / (axes[axes.length - 1].max - axes[axes.length - 1].min)
      points.push({ x: axes.length - 1, y: 1 - scoreRatio })

      // Color by score ratio (considers optimization direction)
      const scoreColorRatio = computeScoreRatio(scores, t.score, optimize)
      return {
        trialIdx,
        points,
        score: t.score,
        slaPass: t.slaPass,
        color: scoreColorToHsl(scoreColorRatio, t.slaPass),
      }
    })

    return { axes, trialLines, allTrials: trials }
  }, [trials, optimize])

  const { w, h } = dimensions
  const padding = { top: 30, right: 60, bottom: 80, left: 20 }
  const plotW = w - padding.left - padding.right
  const plotH = h - padding.top - padding.bottom
  const axisCount = axes.length

  if (axisCount === 0) {
    return (
      <Card>
        <CardHeader><CardTitle>Parallel Coordinates</CardTitle></CardHeader>
        <CardContent><p className="text-muted-foreground text-sm">No parameters to display.</p></CardContent>
      </Card>
    )
  }

  const xForAxis = (i: number) => padding.left + (axisCount <= 1 ? plotW / 2 : (i / (axisCount - 1)) * plotW)

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex flex-col gap-0.5">
          <CardTitle>Parallel Coordinates</CardTitle>
          <div className="h-5">
            {hoveredTrial !== null && (
              <span className="text-sm font-normal text-muted-foreground">
                Trial #{allTrials[hoveredTrial]?.trialIndex} — Score: {formatNumber(allTrials[hoveredTrial]?.score ?? 0)}
              </span>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <svg ref={svgRef} viewBox={`0 0 ${w} ${h}`} className="w-full" style={{ height }}>
          {/* Grid lines */}
          {axes.map((axis, i) => {
            const x = xForAxis(i)
            const steps = 5
            return Array.from({ length: steps }, (_, s) => {
              const y = padding.top + (s / (steps - 1)) * plotH
              return (
                <line
                  key={`grid-${i}-${s}`}
                  x1={x - 4} y1={y} x2={x} y2={y}
                  stroke="hsl(217, 33%, 25%)" strokeWidth={1}
                />
              )
            })
          })}

          {/* Trial lines */}
          {trialLines.map((line, idx) => {
            const pts = line.points.map(p => ({
              x: xForAxis(p.x),
              y: padding.top + p.y * plotH,
            }))
            const d = pointsToGentlePath(pts)

            const isHovered = hoveredTrial === idx
            const opacity = hoveredTrial !== null ? (isHovered ? 1 : 0.08) : 0.7
            const strokeWidth = isHovered ? 3.5 : 1.8

            return (
              <g key={line.trialIdx}>
                {/* Invisible wide hit area for mouse interaction */}
                <path
                  d={d}
                  fill="none"
                  stroke="transparent"
                  strokeWidth={18}
                  onMouseEnter={() => setHoveredTrial(idx)}
                  onMouseLeave={() => setHoveredTrial(null)}
                  style={{ cursor: 'pointer' }}
                />
                <path
                  d={d}
                  fill="none"
                  stroke={line.color}
                  strokeWidth={strokeWidth}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeDasharray={line.slaPass ? undefined : '6,4'}
                  opacity={opacity}
                  className="transition-all duration-200 pointer-events-none"
                />
              </g>
            )
          })}

          {/* Axis lines */}
          {axes.map((axis, i) => {
            const x = xForAxis(i)
            return (
              <line
                key={`axis-${i}`}
                x1={x} y1={padding.top}
                x2={x} y2={padding.top + plotH}
                stroke="hsl(217, 33%, 25%)" strokeWidth={2}
              />
            )
          })}

          {/* Hover dots on the highlighted trial line */}
          {hoveredTrial !== null && trialLines[hoveredTrial] && trialLines[hoveredTrial].points.map((p, pi) => {
            const x = xForAxis(p.x)
            const y = padding.top + p.y * plotH
            return <circle key={`dot-${pi}`} cx={x} cy={y} r={4} fill="white" stroke={trialLines[hoveredTrial].color} strokeWidth={2.5} />
          })}

          {/* Score labels at the rightmost axis */}
          {trialLines.map(line => {
            const lastP = line.points[line.points.length - 1]
            const x = xForAxis(lastP.x)
            const y = padding.top + lastP.y * plotH
            return (
              <text
                key={`score-${line.trialIdx}`}
                x={x + 8}
                y={y + 4}
                className="fill-foreground"
                fontSize="10"
                fontFamily="JetBrains Mono, monospace"
                fontWeight="500"
                opacity={hoveredTrial === null ? 0.85 : (hoveredTrial === line.trialIdx ? 1 : 0.1)}
              >
                {formatNumber(line.score)}
              </text>
            )
          })}

          {/* Axis labels (bottom) */}
          {axes.map((axis, i) => {
            const x = xForAxis(i)
            const label = axis.isMetric ? optimize : axis.label
            return (
              <text
                key={`label-${i}`}
                x={x}
                y={padding.top + plotH + 32}
                textAnchor="middle"
                className="fill-muted-foreground"
                fontSize="11"
                fontFamily="Inter, sans-serif"
              >
                {axisLabelShort(label)}
              </text>
            )
          })}

          {/* Top tick labels (min/max values) */}
          {axes.map((axis, i) => {
            const x = xForAxis(i)
            return (
              <g key={`ticks-${i}`}>
                <text
                  x={x}
                  y={padding.top - 5}
                  textAnchor="middle"
                  className="fill-muted-foreground"
                  fontSize="9"
                  fontFamily="JetBrains Mono, monospace"
                >
                  {formatNumber(axis.max)}
                </text>
                <text
                  x={x}
                  y={padding.top + plotH + 14}
                  textAnchor="middle"
                  className="fill-muted-foreground"
                  fontSize="9"
                  fontFamily="JetBrains Mono, monospace"
                >
                  {formatNumber(axis.min)}
                </text>
              </g>
            )
          })}
        </svg>

        {/* Color legend */}
        <div className="flex items-center justify-center gap-2 mt-2">
          <span className="text-xs text-muted-foreground">Low Score</span>
          <div
            className="h-2.5 w-32 rounded-full"
            style={{ background: 'linear-gradient(90deg, hsl(120, 65%, 48%), hsl(0, 65%, 48%))' }}
          />
          <span className="text-xs text-muted-foreground">High Score</span>
        </div>
        {/* SLA fail hint */}
        <div className="flex items-center justify-center gap-2 mt-1.5">
          <svg width="24" height="12" className="shrink-0">
            <line x1="0" y1="6" x2="20" y2="6" stroke="hsl(0, 0%, 45%)" strokeWidth="2" strokeDasharray="6,4" />
          </svg>
          <span className="text-xs text-muted-foreground">SLA Failed (dashed gray)</span>
        </div>
      </CardContent>
    </Card>
  )
}

function axisLabelShort(label: string): string {
  // Shorten common parameter names for display
  const map: Record<string, string> = {
    'gpuMemoryUtilization': 'gpuMem',
    'maxNumSeqs': 'maxSeqs',
    'outputThroughput': 'outTok/s',
    'inputThroughput': 'inTok/s',
    'totalThroughput': 'totTok/s',
    'requestsPerSecond': 'req/s',
  }
  return map[label] || label
}

/**
 * Convert points to a smooth path using Catmull-Rom spline.
 * Ensures C1 continuity at each data point (smooth tangent through all points).
 * Uses very small tension (0.1) for curves very close to straight lines,
 * matching Optuna's parallel coordinate plot style.
 */
function pointsToGentlePath(points: { x: number; y: number }[]): string {
  if (points.length < 2) return ''
  if (points.length === 2) return `M${points[0].x},${points[0].y}L${points[1].x},${points[1].y}`

  const tension = 0.15
  let d = `M${points[0].x},${points[0].y}`

  for (let i = 0; i < points.length - 1; i++) {
    const p0 = points[Math.max(i - 1, 0)]
    const p1 = points[i]
    const p2 = points[i + 1]
    const p3 = points[Math.min(i + 2, points.length - 1)]

    // Catmull-Rom to cubic bezier conversion
    const cp1x = p1.x + (p2.x - p0.x) * tension
    const cp1y = p1.y + (p2.y - p0.y) * tension
    const cp2x = p2.x - (p3.x - p1.x) * tension
    const cp2y = p2.y - (p3.y - p1.y) * tension

    d += `C${cp1x},${cp1y} ${cp2x},${cp2y} ${p2.x},${p2.y}`
  }

  return d
}
