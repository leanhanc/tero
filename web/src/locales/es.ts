import type { Messages } from './en';

export const es: Messages = {
	common: {
		loading: 'Cargando…',
		saving: 'Guardando…',
	},
	errors: {
		generic: 'Algo salió mal. Volvé a intentarlo.',
		unreachable: 'No se pudo conectar con el servidor. Revisá tu conexión y volvé a intentarlo.',
	},
	header: {
		home: 'Tero',
		logOut: 'Cerrar sesión',
	},
	addressHelp: {
		label: '¿Qué es nip.io?',
		explanation:
			'nip.io es un servicio de DNS público y gratuito. Responde a un nombre que lleva una dirección IP, como shop.203.0.113.10.nip.io, con esa IP, así tu app tiene una dirección HTTPS que funciona antes de que configures un dominio propio. Los visitantes se conectan directo a tu servidor; ningún tráfico pasa por nip.io.',
	},
	setup: {
		heading: 'Configurá el acceso al panel',
		missingToken: 'Esta página necesita el enlace de configuración que se imprimió en el servidor. Ejecutá sudo tero reset-login ahí para obtener uno nuevo.',
		password: {
			heading: 'Elegí una contraseña',
			label: 'Contraseña',
			hint: 'Al menos 8 caracteres.',
			submit: 'Continuar',
		},
		authenticator: {
			heading: 'Agregá Tero a tu app de autenticación',
			scanInstructions: 'Escaneá este código QR con una app de autenticación, como 1Password, Bitwarden o Google Authenticator.',
			qrCodeAlt: 'Código QR que agrega Tero a una app de autenticación',
			manualKey: '¿No podés escanearlo? Ingresá esta clave en la app:',
			codeLabel: 'Código de 6 dígitos de la app',
			submit: 'Terminar la configuración',
		},
	},
	login: {
		heading: 'Iniciá sesión en Tero',
		passwordLabel: 'Contraseña',
		codeLabel: 'Código de 6 dígitos de tu app de autenticación',
		submit: 'Iniciar sesión',
		notSetUp: {
			beforeInit: 'El acceso al panel todavía no está configurado. Abrí el enlace de configuración que imprimió en el servidor el comando',
			beforeReset: ', o ejecutá',
			afterReset: 'ahí para obtener uno nuevo.',
		},
	},
	home: {
		heading: 'Apps',
		apps: {
			label: 'Tus apps',
			deployNew: 'Desplegar una app nueva',
			empty: 'Todavía no hay apps. Desplegá una desde un repositorio público de GitHub para empezar.',
			appColumn: 'App',
			addressColumn: 'Dirección',
			latestDeployColumn: 'Último despliegue',
		},
	},
	newApp: {
		heading: 'Desplegar una app nueva',
		intro: 'Tero construye la app desde un repositorio público de GitHub y le da su propia dirección HTTPS.',
		repository: {
			label: 'Repositorio de GitHub',
			placeholder: 'https://github.com/dueño/repositorio',
			hint: 'Por ahora, solo repositorios públicos.',
		},
		branch: {
			label: 'Rama',
			hint: 'Tero despliega el último commit de esta rama.',
		},
		displayName: {
			label: 'Nombre',
			hint: 'Cómo llama el panel a la app. Podés cambiarlo después.',
		},
		address: {
			label: 'Dirección',
			preview: (address) => `Va a ser ${address}. `,
			hint: 'Letras minúsculas, dígitos y guiones. No se puede cambiar después.',
		},
		rootDirectory: {
			label: 'Carpeta en el repositorio (opcional)',
			hint: 'Dejalo vacío salvo que la app esté en una subcarpeta, como apps/api.',
		},
		submit: 'Crear y desplegar',
		submitting: 'Revisando el repositorio…',
	},
	app: {
		summary: {
			address: 'Dirección',
			repository: 'Repositorio',
			branch: 'Rama',
			folder: 'Carpeta',
			repositoryRoot: 'La raíz del repositorio',
			liveCommit: 'Commit en vivo',
			nothingLive: 'Todavía no hay nada en vivo',
		},
		redeploy: {
			label: 'Volver a desplegar',
			submit: 'Volver a desplegar',
			hint: 'Construye y despliega el último commit de la rama.',
			inProgress: 'Hay un despliegue en curso.',
		},
		displayName: {
			formLabel: 'Nombre de la app',
			label: 'Nombre',
			hint: 'Cómo llama el panel a la app. Cambiarlo no cambia la dirección.',
			submit: 'Guardar nombre',
		},
		runTogether: {
			sectionLabel: 'Ajustes de despliegue',
			label: 'Correr la versión vieja y la nueva juntas durante los despliegues',
			hint: 'Si está activado, la versión nueva arranca al lado de la vieja y la reemplaza cuando responde. Desactivalo para apps que no pueden correr dos veces a la vez: la versión vieja se detiene primero, así que la app queda caída un momento. En los dos casos, si la versión nueva falla, la vieja sigue atendiendo.',
		},
		deploys: {
			empty: 'Todavía no hay despliegues.',
			caption: 'Despliegues, del más reciente al más antiguo',
			startedColumn: 'Inicio',
			commitColumn: 'Commit',
			stateColumn: 'Estado',
			logColumn: 'Registro',
			commitNotFetched: 'Todavía no se descargó',
			showLog: 'Ver registro',
		},
		buildLog: {
			heading: (deployID) => `Registro de construcción del despliegue ${deployID}`,
			streaming: 'Siguiendo la construcción mientras corre…',
			unavailable: 'No se pudo leer el registro.',
			empty: 'Todavía no hay salida.',
		},
	},
	deployStates: {
		notDeployed: 'Todavía no se desplegó',
		states: {
			queued: 'Esperando para empezar',
			building: 'Construyendo',
			starting: 'Arrancando',
			live: 'En vivo',
			failed: 'Falló',
			superseded: 'Reemplazado por un despliegue más nuevo',
		},
		failedSteps: {
			building: 'durante la construcción',
			starting: 'durante el arranque',
		},
	},
};
