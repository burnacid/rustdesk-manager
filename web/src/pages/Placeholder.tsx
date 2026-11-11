export function Placeholder({ title, milestone }: { title: string; milestone: number }) {
  return (
    <div>
      <h1 className="mb-2 text-2xl font-semibold">{title}</h1>
      {milestone > 0 && <p className="text-slate-500 dark:text-slate-400">Coming in milestone {milestone}.</p>}
    </div>
  );
}
