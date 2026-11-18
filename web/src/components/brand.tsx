import { TeroMark } from './icons';

type BrandProps = {
	/** Centres the logo alone, with the name only read aloud, as on the pages before signing in. */
	isMarkOnly?: boolean;
};

/** Tero's logo and name. On narrow screens the name is only read aloud. */
export function Brand({ isMarkOnly = false }: BrandProps) {
	return (
		<div className={isMarkOnly ? 'brand center' : 'brand'}>
			<span className="t-logo">
				<TeroMark />
			</span>
			<span className={isMarkOnly ? 'visually-hidden' : 'brand-name'}>Tero</span>
		</div>
	);
}
