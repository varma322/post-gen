export default function PageHeader({ title, subtitle, action }) {
  return (
    <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 border-b border-outline-variant pb-6">
      <div>
        <h1 className="text-3xl font-headline font-bold text-on-surface">{title}</h1>
        <p className="text-on-surface-variant mt-1 font-body">{subtitle}</p>
      </div>
      {action}
    </div>
  );
}
