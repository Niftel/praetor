import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const config: Config = {
  title: 'Praetor',
  tagline: 'Push a self-contained execution environment. Never install Ansible on a target.',
  favicon: 'img/favicon.ico',

  future: {
    v4: true,
  },

  url: 'https://docs.localhost',
  baseUrl: '/',

  organizationName: 'praetordev',
  projectName: 'praetor',

  // Docs are the whole site; broken links warn (don't fail the build) while docs evolve.
  onBrokenLinks: 'warn',

  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          routeBasePath: '/', // serve docs at the site root (no /docs prefix)
          editUrl: 'https://github.com/praetordev/praetor/tree/main/docs-site/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/docusaurus-social-card.jpg',
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Praetor',
      logo: {
        alt: 'Praetor',
        src: 'img/logo.svg',
      },
      items: [
        {type: 'docSidebar', sidebarId: 'docs', position: 'left', label: 'Docs'},
        {href: 'https://github.com/praetordev/praetor', label: 'GitHub', position: 'right'},
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Introduction', to: '/'},
            {label: 'Getting Started', to: '/getting-started'},
          ],
        },
        {
          title: 'Reference',
          items: [
            {label: 'Execution Packs', to: '/concepts/execution-packs'},
            {label: 'API & Auth', to: '/api/authentication'},
          ],
        },
      ],
      copyright: 'Praetor — self-contained, pushable Ansible automation.',
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'json', 'go'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
