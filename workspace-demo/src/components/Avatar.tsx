import React from 'react';
import { initialsOf } from '../lib/format';

interface AvatarProps {
  name: string;
  src?: string;
  className?: string;
}

export const Avatar: React.FC<AvatarProps> = ({ name, src, className }) => {
  if (src) {
    return <img src={src} alt={name} className={`rounded-full object-cover ${className ?? ''}`} />;
  }
  return (
    <div
      aria-hidden="true"
      className={`rounded-full bg-brand-500/10 text-brand-600 dark:text-brand-400 flex items-center justify-center font-bold ${className ?? ''}`}
    >
      {initialsOf(name)}
    </div>
  );
};
